package board

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/boboty/agent-board/internal/clock"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ids"

	_ "modernc.org/sqlite"
)

const actor = "test-orchestrator"

type fixture struct {
	path      string
	projectID string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	generator, err := ids.NewGenerator(clock.RealClock{}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	return fixture{path: filepath.Join(t.TempDir(), "board.db"), projectID: projectID}
}

func (f fixture) open(t *testing.T) *Service {
	t.Helper()
	s, err := Open(context.Background(), Config{DatabasePath: f.path, ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s
}

func newService(t *testing.T) (*Service, fixture) {
	f := newFixture(t)
	return f.open(t), f
}

// raw opens an independent connection for inspection or fault injection.
func (f fixture) raw(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(f.path)+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustCreate(t *testing.T, s *Service, title string) domain.Task {
	t.Helper()
	task, err := s.CreateTask(context.Background(), CreateTaskInput{Actor: actor, Title: title})
	if err != nil {
		t.Fatalf("CreateTask(%q) error = %v", title, err)
	}
	return task
}

func mustQueue(t *testing.T, s *Service, task domain.Task) domain.Task {
	t.Helper()
	queued, err := s.QueueTask(context.Background(), QueueTaskInput{Actor: actor, Task: task.ID, ExpectedVersion: task.Version})
	if err != nil {
		t.Fatalf("QueueTask(#%d) error = %v", task.Number, err)
	}
	return queued
}

func mustSetState(t *testing.T, s *Service, task domain.Task, state domain.State, reason *string) domain.Task {
	t.Helper()
	updated, err := s.SetTaskState(context.Background(), SetTaskStateInput{Actor: actor, Task: task.ID, ExpectedVersion: task.Version, State: state, Reason: reason})
	if err != nil {
		t.Fatalf("SetTaskState(#%d, %s) error = %v", task.Number, state, err)
	}
	return updated
}

func mustGet(t *testing.T, s *Service, ref string) domain.Task {
	t.Helper()
	task, err := s.GetTask(context.Background(), ref)
	if err != nil {
		t.Fatalf("GetTask(%q) error = %v", ref, err)
	}
	return task
}

func readyIDs(t *testing.T, s *Service) (int64, []string) {
	t.Helper()
	queue, err := s.ListReady(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(queue.Tasks))
	for i, task := range queue.Tasks {
		out[i] = task.ID
	}
	return queue.Version, out
}

func taskEvents(t *testing.T, s *Service, ref string) []domain.TaskEvent {
	t.Helper()
	events, err := s.ListEvents(context.Background(), ListEventsInput{Task: ref, Limit: MaxEventLimit})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if !domain.IsCode(err, code) {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func ptr[T any](v T) *T { return &v }

// assertAuditConsistent checks, straight from storage, that every task's
// version is explained by exactly its recorded mutation events, every fact
// has exactly one fact_recorded event, and READY ranks are coherent.
func assertAuditConsistent(t *testing.T, f fixture) {
	t.Helper()
	db := f.raw(t)
	checks := map[string]string{
		"task version != 1 + mutation events": `SELECT count(*) FROM tasks t WHERE t.version != 1 + (
			SELECT count(*) FROM task_events e WHERE e.task_id = t.id
			AND e.type IN ('task_updated', 'task_queued', 'task_state_set'))`,
		"task without exactly one task_created event": `SELECT count(*) FROM tasks t WHERE 1 != (
			SELECT count(*) FROM task_events e WHERE e.task_id = t.id AND e.type = 'task_created')`,
		"latest task event version != task version": `SELECT count(*) FROM tasks t WHERE t.version != (
			SELECT e.task_version FROM task_events e WHERE e.task_id = t.id ORDER BY e.id DESC LIMIT 1)`,
		"fact without exactly one fact_recorded event": `SELECT count(*) FROM task_facts f WHERE 1 != (
			SELECT count(*) FROM task_events e WHERE e.type = 'fact_recorded'
			AND json_extract(e.payload, '$.fact_id') = f.id AND e.task_id = f.task_id)`,
		"fact_recorded event without fact": `SELECT count(*) FROM task_events e WHERE e.type = 'fact_recorded'
			AND NOT EXISTS (SELECT 1 FROM task_facts f WHERE f.id = json_extract(e.payload, '$.fact_id'))`,
		"queued task without rank": `SELECT count(*) FROM tasks WHERE (queued_at IS NULL) != (ready_rank IS NULL)`,
	}
	for name, query := range checks {
		var n int
		if err := db.QueryRow(query).Scan(&n); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if n != 0 {
			t.Errorf("audit inconsistency: %s (%d rows)", name, n)
		}
	}
}

func decodePayload(t *testing.T, event domain.TaskEvent) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

// stateOf returns a task's lifecycle state, or "" for an unqueued task.
func stateOf(task domain.Task) domain.State {
	if task.State == nil {
		return ""
	}
	return *task.State
}
