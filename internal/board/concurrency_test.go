package board

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/sqlite"
)

const (
	helperEnv     = "AGENT_BOARD_TEST_HELPER"
	helperDBEnv   = "AGENT_BOARD_TEST_DB"
	helperProjEnv = "AGENT_BOARD_TEST_PROJECT"
	helperIDEnv   = "AGENT_BOARD_TEST_WORKER"
)

// workload is one harness's share of contended Board traffic. Every worker
// appends one character per round to a shared task's description with a
// read-check-write loop (lost updates would shorten the result), creates and
// queues its own task, records facts (one retried with the same idempotency
// key), and moves its task through states. VERSION_CONFLICT is the only
// error a worker expects; it re-reads and retries.
func workload(ctx context.Context, s *Service, worker string, shared string, rounds int) error {
	for round := 0; round < rounds; round++ {
		for {
			task, err := s.GetTask(ctx, shared)
			if err != nil {
				return err
			}
			_, err = s.UpdateTask(ctx, UpdateTaskInput{Actor: worker, Task: shared, ExpectedVersion: task.Version, Description: ptr(task.Description + "x")})
			if err == nil {
				break
			}
			if !domain.IsCode(err, domain.CodeVersionConflict) {
				return fmt.Errorf("shared update: %w", err)
			}
		}

		task, err := s.CreateTask(ctx, CreateTaskInput{Actor: worker, IdempotencyKey: fmt.Sprintf("%s-create-%d", worker, round), Title: fmt.Sprintf("%s task %d", worker, round)})
		if err != nil {
			return fmt.Errorf("create: %w", err)
		}
		if task, err = s.QueueTask(ctx, QueueTaskInput{Actor: worker, Task: task.ID, ExpectedVersion: task.Version}); err != nil {
			return fmt.Errorf("queue: %w", err)
		}
		factIn := RecordFactInput{Actor: worker, IdempotencyKey: fmt.Sprintf("%s-fact-%d", worker, round), Task: task.ID, Kind: domain.FactExecution, Body: "started", Data: []byte(`{"worker":"` + worker + `"}`)}
		for retry := 0; retry < 2; retry++ {
			if _, err := s.RecordFact(ctx, factIn); err != nil {
				return fmt.Errorf("fact: %w", err)
			}
		}
		if task, err = s.SetTaskState(ctx, SetTaskStateInput{Actor: worker, Task: task.ID, ExpectedVersion: task.Version, State: domain.StateInProgress}); err != nil {
			return fmt.Errorf("start: %w", err)
		}
		if round%2 == 0 {
			if _, err = s.SetTaskState(ctx, SetTaskStateInput{Actor: worker, Task: task.ID, ExpectedVersion: task.Version, State: domain.StateReady}); err != nil {
				return fmt.Errorf("requeue: %w", err)
			}
		}
		if err := reorderReverse(ctx, s, worker); err != nil {
			return err
		}
	}
	return nil
}

// reorderReverse reverses the READY queue, retrying on READY_ORDER_CONFLICT.
func reorderReverse(ctx context.Context, s *Service, worker string) error {
	for {
		queue, err := s.ListReady(ctx)
		if err != nil {
			return err
		}
		order := make([]string, len(queue.Tasks))
		for i, task := range queue.Tasks {
			order[len(order)-1-i] = task.ID
		}
		_, err = s.ReorderReady(ctx, ReorderReadyInput{Actor: worker, ExpectedVersion: queue.Version, Tasks: order})
		if err == nil {
			return nil
		}
		if !domain.IsCode(err, domain.CodeReadyOrderConflict) {
			return fmt.Errorf("reorder: %w", err)
		}
	}
}

func verifyWorkload(t *testing.T, f fixture, workers, rounds int, shared string) {
	t.Helper()
	s := f.open(t)
	ctx := context.Background()
	task := mustGet(t, s, shared)
	if len(task.Description) != workers*rounds || task.Version != int64(1+workers*rounds) {
		t.Fatalf("shared task description len=%d version=%d, want %d / %d (lost or duplicated updates)",
			len(task.Description), task.Version, workers*rounds, 1+workers*rounds)
	}
	all, _ := s.ListTasks(ctx, ListTasksInput{})
	if len(all) != 1+workers*rounds {
		t.Fatalf("tasks = %d, want %d", len(all), 1+workers*rounds)
	}
	seenNumbers := map[int64]bool{}
	for _, task := range all {
		if seenNumbers[task.Number] {
			t.Fatalf("duplicate task number %d", task.Number)
		}
		seenNumbers[task.Number] = true
		if task.ID == shared {
			continue
		}
		facts, _ := s.ListFacts(ctx, ListFactsInput{Task: task.ID})
		if len(facts) != 1 {
			t.Fatalf("task #%d facts = %d, want 1 (idempotent retry applied twice?)", task.Number, len(facts))
		}
	}
	queue, _ := s.ListReady(ctx)
	wantReady := workers * ((rounds + 1) / 2)
	if len(queue.Tasks) != wantReady {
		t.Fatalf("READY queue = %d, want %d", len(queue.Tasks), wantReady)
	}
	assertAuditConsistent(t, f)
}

// TestConcurrentServicesShareOneDatabase runs several independent Services
// (separate connection pools, as separate harnesses would have) against one
// file concurrently.
func TestConcurrentServicesShareOneDatabase(t *testing.T) {
	f := newFixture(t)
	shared := mustCreate(t, f.open(t), "shared").ID
	const workers, rounds = 6, 15
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	start := make(chan struct{})
	for w := 0; w < workers; w++ {
		s := f.open(t)
		wg.Add(1)
		go func(worker string) {
			defer wg.Done()
			<-start
			if err := workload(context.Background(), s, worker, shared, rounds); err != nil {
				errs <- fmt.Errorf("%s: %w", worker, err)
			}
		}(fmt.Sprintf("harness-%d", w))
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	verifyWorkload(t, f, workers, rounds, shared)
}

// TestMultiProcessWriters runs the same workload in separate OS processes.
func TestMultiProcessWriters(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	f := newFixture(t)
	shared := mustCreate(t, f.open(t), "shared").ID
	const workers, rounds = 4, 12
	commands := make([]*exec.Cmd, workers)
	outputs := make([]*strings.Builder, workers)
	for w := range commands {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		cmd.Env = append(os.Environ(), helperEnv+"=workload", helperDBEnv+"="+f.path, helperProjEnv+"="+f.projectID,
			helperIDEnv+"=process-"+strconv.Itoa(w), "AGENT_BOARD_TEST_SHARED="+shared, "AGENT_BOARD_TEST_ROUNDS="+strconv.Itoa(rounds))
		outputs[w] = &strings.Builder{}
		cmd.Stdout, cmd.Stderr = outputs[w], outputs[w]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands[w] = cmd
	}
	for w, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("process %d failed: %v\n%s", w, err, outputs[w])
		}
	}
	verifyWorkload(t, f, workers, rounds, shared)
}

// TestKilledWriterLeavesNoPartialCommit kills a process holding an open
// write transaction that has already changed a task and appended its event,
// then checks that nothing was committed and the write lock was released.
func TestKilledWriterLeavesNoPartialCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	f := newFixture(t)
	s := f.open(t)
	task := mustQueue(t, s, mustCreate(t, s, "t"))

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"=hang-in-write", helperDBEnv+"="+f.path, "AGENT_BOARD_TEST_SHARED="+task.ID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "in-transaction" {
		_ = cmd.Process.Kill()
		t.Fatalf("helper did not reach transaction: %q %v", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	stored := mustGet(t, s, task.ID)
	if stateOf(stored) != domain.StateReady || stored.Version != 2 {
		t.Fatalf("killed transaction leaked: %+v", stored)
	}
	if events := taskEvents(t, s, task.ID); len(events) != 2 {
		t.Fatalf("killed transaction leaked events: %d", len(events))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := s.SetTaskState(ctx, SetTaskStateInput{Actor: actor, Task: task.ID, ExpectedVersion: 2, State: domain.StateInProgress}); err != nil {
		t.Fatalf("write after killed writer: %v", err)
	}
	assertAuditConsistent(t, f)
}

// TestHelperProcess is the subprocess entry point for the tests above; it
// does nothing in a normal test run.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		t.Skip("helper process only")
	}
	ctx := context.Background()
	switch mode {
	case "workload":
		rounds, _ := strconv.Atoi(os.Getenv("AGENT_BOARD_TEST_ROUNDS"))
		s, err := Open(ctx, Config{DatabasePath: os.Getenv(helperDBEnv), ProjectID: os.Getenv(helperProjEnv)})
		if err != nil {
			fmt.Println("open:", err)
			os.Exit(2)
		}
		defer s.Close(ctx)
		if err := workload(ctx, s, os.Getenv(helperIDEnv), os.Getenv("AGENT_BOARD_TEST_SHARED"), rounds); err != nil {
			fmt.Println("workload:", err)
			os.Exit(3)
		}
	case "hang-in-write":
		db, err := sqlite.Open(ctx, os.Getenv(helperDBEnv), sqlite.Options{})
		if err != nil {
			fmt.Println("open:", err)
			os.Exit(2)
		}
		err = db.Write(ctx, func(ctx context.Context, tx sqlite.Executor) error {
			id := os.Getenv("AGENT_BOARD_TEST_SHARED")
			if _, err := tx.ExecContext(ctx, "UPDATE tasks SET state = 'DONE', version = version + 1 WHERE id = ?", id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO task_events(task_id, type, actor, task_version, payload, created_at)
				VALUES (?, 'task_state_set', 'doomed', 3, '{}', '2026-01-01T00:00:00.000000000Z')`, id); err != nil {
				return err
			}
			fmt.Println("in-transaction")
			time.Sleep(time.Hour)
			return nil
		})
		fmt.Println("unexpected:", errors.Join(err))
		os.Exit(4)
	}
}
