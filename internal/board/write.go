package board

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/sqlite"
)

// mutate runs fn in one write transaction together with the idempotency
// lookup and record for key. fn runs at most once per committed result but
// may be re-invoked after a rolled-back BUSY attempt, so it must not touch Go
// state outside its own body.
func mutate[T any](ctx context.Context, s *Service, operation, key string, request any,
	fn func(context.Context, sqlite.Executor, time.Time) (T, error),
) (T, error) {
	var hash []byte
	if key != "" {
		encoded, err := json.Marshal(request)
		if err != nil {
			var zero T
			return zero, err
		}
		sum := sha256.Sum256(append([]byte(operation+"\x00"), encoded...))
		hash = sum[:]
	}

	var result T
	err := s.db.Write(ctx, func(ctx context.Context, tx sqlite.Executor) error {
		var zero T
		result = zero
		now := s.clock.Now()
		if key != "" {
			var storedHash []byte
			var response string
			err := tx.QueryRowContext(ctx, `SELECT request_hash, response_json FROM idempotency_records
				WHERE operation = ? AND idempotency_key = ?`, operation, key).Scan(&storedHash, &response)
			switch {
			case err == nil:
				if !bytes.Equal(storedHash, hash) {
					return domain.NewError(domain.CodeIdempotencyConflict,
						"idempotency key was already used with a different "+operation+" request", false)
				}
				return json.Unmarshal([]byte(response), &result)
			case !isNoRows(err):
				return err
			}
		}
		value, err := fn(ctx, tx, now)
		if err != nil {
			return err
		}
		if key != "" {
			response, err := json.Marshal(value)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(
				operation, idempotency_key, request_hash, response_json, created_at) VALUES (?, ?, ?, ?, ?)`,
				operation, key, hash, string(response), sqlite.FormatTime(now)); err != nil {
				return err
			}
		}
		result = value
		return nil
	})
	return result, err
}

// CreateTask creates an unqueued task. It has no lifecycle state until it is
// queued.
func (s *Service) CreateTask(ctx context.Context, in CreateTaskInput) (domain.Task, error) {
	if err := in.normalize(); err != nil {
		return domain.Task{}, err
	}
	id, err := s.newID()
	if err != nil {
		return domain.Task{}, err
	}
	return mutate(ctx, s, "create_task", in.IdempotencyKey, in, func(ctx context.Context, tx sqlite.Executor, now time.Time) (domain.Task, error) {
		var number int64
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(number), 0) + 1 FROM tasks").Scan(&number); err != nil {
			return domain.Task{}, err
		}
		task := domain.Task{
			ID:                 id,
			Number:             number,
			Title:              in.Title,
			Description:        in.Description,
			AcceptanceCriteria: in.AcceptanceCriteria,
			Version:            1,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tasks(id, number, title, description, acceptance_criteria,
			version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
			task.ID, task.Number, task.Title, task.Description, task.AcceptanceCriteria,
			sqlite.FormatTime(now), sqlite.FormatTime(now)); err != nil {
			return domain.Task{}, err
		}
		if err := appendEvent(ctx, tx, &task, domain.EventTaskCreated, in.Actor, map[string]any{"task": task}, now); err != nil {
			return domain.Task{}, err
		}
		return loadTask(ctx, tx, task.ID)
	})
}

// UpdateTask edits title, description, or acceptance criteria. A request
// that changes nothing returns the task without a new version or event.
func (s *Service) UpdateTask(ctx context.Context, in UpdateTaskInput) (domain.Task, error) {
	if err := in.normalize(); err != nil {
		return domain.Task{}, err
	}
	return mutate(ctx, s, "update_task", in.IdempotencyKey, in, func(ctx context.Context, tx sqlite.Executor, now time.Time) (domain.Task, error) {
		task, err := loadForMutation(ctx, tx, in.Task, in.ExpectedVersion)
		if err != nil {
			return domain.Task{}, err
		}
		changes := map[string]any{}
		apply := func(field string, current *string, next *string) {
			if next != nil && *next != *current {
				changes[field] = map[string]string{"from": *current, "to": *next}
				*current = *next
			}
		}
		apply("title", &task.Title, in.Title)
		apply("description", &task.Description, in.Description)
		apply("acceptance_criteria", &task.AcceptanceCriteria, in.AcceptanceCriteria)
		if len(changes) == 0 {
			return task, nil
		}
		if err := updateTaskRow(ctx, tx, task.ID, task.Version, now,
			"title = ?, description = ?, acceptance_criteria = ?", task.Title, task.Description, task.AcceptanceCriteria); err != nil {
			return domain.Task{}, err
		}
		task.Version++
		if err := appendEvent(ctx, tx, &task, domain.EventTaskUpdated, in.Actor, map[string]any{"changes": changes}, now); err != nil {
			return domain.Task{}, err
		}
		return loadTask(ctx, tx, task.ID)
	})
}

// QueueTask queues an unqueued task: it explicitly records state READY and
// appends the task to the end of the READY ordering.
func (s *Service) QueueTask(ctx context.Context, in QueueTaskInput) (domain.Task, error) {
	if err := in.normalize(); err != nil {
		return domain.Task{}, err
	}
	return mutate(ctx, s, "queue_task", in.IdempotencyKey, in, func(ctx context.Context, tx sqlite.Executor, now time.Time) (domain.Task, error) {
		task, err := loadForMutation(ctx, tx, in.Task, in.ExpectedVersion)
		if err != nil {
			return domain.Task{}, err
		}
		if task.QueuedAt != nil {
			return domain.Task{}, domain.NewError(domain.CodeAlreadyQueued, fmt.Sprintf("task #%d is already queued", task.Number), false)
		}
		var rank int64
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(ready_rank), 0) + 1 FROM tasks").Scan(&rank); err != nil {
			return domain.Task{}, err
		}
		if err := updateTaskRow(ctx, tx, task.ID, task.Version, now,
			"state = ?, queued_at = ?, ready_rank = ?", domain.StateReady, sqlite.FormatTime(now), rank); err != nil {
			return domain.Task{}, err
		}
		if err := bumpReadyVersion(ctx, tx); err != nil {
			return domain.Task{}, err
		}
		task.Version++
		if err := appendEvent(ctx, tx, &task, domain.EventTaskQueued, in.Actor,
			map[string]any{"queued_at": now, "ready_rank": rank, "state": domain.StateReady}, now); err != nil {
			return domain.Task{}, err
		}
		return loadTask(ctx, tx, task.ID)
	})
}

// SetTaskState records the requested task-level state on a queued task. The
// Board applies no transition policy: which role may set which state, and
// when, is defined by the Workflow Skill. An unqueued task has no lifecycle
// yet; it enters READY only through QueueTask.
func (s *Service) SetTaskState(ctx context.Context, in SetTaskStateInput) (domain.Task, error) {
	if err := in.normalize(); err != nil {
		return domain.Task{}, err
	}
	return mutate(ctx, s, "set_task_state", in.IdempotencyKey, in, func(ctx context.Context, tx sqlite.Executor, now time.Time) (domain.Task, error) {
		task, err := loadForMutation(ctx, tx, in.Task, in.ExpectedVersion)
		if err != nil {
			return domain.Task{}, err
		}
		if task.State == nil {
			return domain.Task{}, domain.NewError(domain.CodeTaskNotQueued,
				fmt.Sprintf("task #%d has no lifecycle state until it is queued", task.Number), false)
		}
		if err := updateTaskRow(ctx, tx, task.ID, task.Version, now,
			"state = ?, state_reason = ?", in.State, nullableString(in.Reason)); err != nil {
			return domain.Task{}, err
		}
		if err := bumpReadyVersion(ctx, tx); err != nil {
			return domain.Task{}, err
		}
		from := *task.State
		task.Version++
		if err := appendEvent(ctx, tx, &task, domain.EventTaskStateSet, in.Actor,
			map[string]any{"from": from, "to": in.State, "reason": in.Reason}, now); err != nil {
			return domain.Task{}, err
		}
		return loadTask(ctx, tx, task.ID)
	})
}

// ReorderReady atomically replaces the order of the READY queue. Tasks that
// are queued but not currently READY keep their positions.
func (s *Service) ReorderReady(ctx context.Context, in ReorderReadyInput) (domain.ReadyQueue, error) {
	in.Tasks = append([]string(nil), in.Tasks...)
	if err := in.normalize(); err != nil {
		return domain.ReadyQueue{}, err
	}
	return mutate(ctx, s, "reorder_ready", in.IdempotencyKey, in, func(ctx context.Context, tx sqlite.Executor, now time.Time) (domain.ReadyQueue, error) {
		current, err := loadReadyQueue(ctx, tx)
		if err != nil {
			return domain.ReadyQueue{}, err
		}
		if current.Version != in.ExpectedVersion {
			return domain.ReadyQueue{}, domain.NewError(domain.CodeReadyOrderConflict,
				fmt.Sprintf("READY queue version is %d, expected %d", current.Version, in.ExpectedVersion), true)
		}
		members := make(map[string]int64, len(current.Tasks))
		from := make([]string, len(current.Tasks))
		ranks := make([]int64, len(current.Tasks))
		for i, task := range current.Tasks {
			members[task.ID] = *task.ReadyRank
			from[i] = task.ID
			ranks[i] = *task.ReadyRank
		}
		to := make([]string, 0, len(in.Tasks))
		seen := make(map[string]bool, len(in.Tasks))
		for _, ref := range in.Tasks {
			id, err := resolveTaskID(ctx, tx, ref)
			if err != nil {
				return domain.ReadyQueue{}, err
			}
			if _, ok := members[id]; !ok || seen[id] {
				return domain.ReadyQueue{}, readySetMismatch()
			}
			seen[id] = true
			to = append(to, id)
		}
		if len(to) != len(from) {
			return domain.ReadyQueue{}, readySetMismatch()
		}
		// Reassign the queue's existing ranks (ascending) in the new order.
		// Ranks go negative first so the unique rank index never sees a
		// transient duplicate.
		for _, id := range from {
			if _, err := tx.ExecContext(ctx, "UPDATE tasks SET ready_rank = -ready_rank WHERE id = ?", id); err != nil {
				return domain.ReadyQueue{}, err
			}
		}
		for i, id := range to {
			if _, err := tx.ExecContext(ctx, "UPDATE tasks SET ready_rank = ? WHERE id = ?", ranks[i], id); err != nil {
				return domain.ReadyQueue{}, err
			}
		}
		if err := bumpReadyVersion(ctx, tx); err != nil {
			return domain.ReadyQueue{}, err
		}
		if err := appendEvent(ctx, tx, nil, domain.EventReadyReordered, in.Actor,
			map[string]any{"from": from, "to": to, "ready_version": current.Version + 1}, now); err != nil {
			return domain.ReadyQueue{}, err
		}
		return loadReadyQueue(ctx, tx)
	})
}

// RecordFact appends an immutable fact. It never changes task state, content,
// or version.
func (s *Service) RecordFact(ctx context.Context, in RecordFactInput) (domain.TaskFact, error) {
	if err := in.normalize(); err != nil {
		return domain.TaskFact{}, err
	}
	id, err := s.newID()
	if err != nil {
		return domain.TaskFact{}, err
	}
	return mutate(ctx, s, "record_fact", in.IdempotencyKey, in, func(ctx context.Context, tx sqlite.Executor, now time.Time) (domain.TaskFact, error) {
		taskID, err := resolveTaskID(ctx, tx, in.Task)
		if err != nil {
			return domain.TaskFact{}, err
		}
		task, err := loadTask(ctx, tx, taskID)
		if err != nil {
			return domain.TaskFact{}, err
		}
		fact := domain.TaskFact{ID: id, TaskID: taskID, Kind: in.Kind, Body: in.Body, Data: in.Data,
			Baseline: in.Baseline, Fingerprint: in.Fingerprint, AcceptedCommit: in.AcceptedCommit, Verdict: in.Verdict,
			Provenance: in.Provenance, Actor: in.Actor, CreatedAt: now}
		var role, session, harness, model any
		if in.Provenance != nil {
			role, session, harness, model = in.Provenance.Role, in.Provenance.Session, in.Provenance.Harness, in.Provenance.Model
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_facts(id, task_id, kind, body, data, actor, provenance_role, provenance_session, provenance_harness, provenance_model, baseline, fingerprint, accepted_commit, verdict, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, fact.ID, fact.TaskID, fact.Kind, fact.Body, nullableJSON(fact.Data),
			fact.Actor, role, session, harness, model, nullableString(fact.Baseline), nullableString(fact.Fingerprint),
			nullableString(fact.AcceptedCommit), nullableString(fact.Verdict), sqlite.FormatTime(now)); err != nil {
			return domain.TaskFact{}, err
		}
		if err := appendEvent(ctx, tx, &task, domain.EventFactRecorded, in.Actor,
			map[string]any{"fact_id": fact.ID, "kind": fact.Kind}, now); err != nil {
			return domain.TaskFact{}, err
		}
		return fact, nil
	})
}

func (s *Service) newID() (string, error) {
	id, err := s.ids.New()
	if err != nil {
		return "", domain.WrapError(err, domain.CodeIDGeneration, "cannot generate identifier", false)
	}
	return id, nil
}

// loadForMutation resolves ref and enforces the optimistic version check
// inside the write transaction.
func loadForMutation(ctx context.Context, tx sqlite.Executor, ref string, expectedVersion int64) (domain.Task, error) {
	id, err := resolveTaskID(ctx, tx, ref)
	if err != nil {
		return domain.Task{}, err
	}
	task, err := loadTask(ctx, tx, id)
	if err != nil {
		return domain.Task{}, err
	}
	if task.Version != expectedVersion {
		return domain.Task{}, domain.NewError(domain.CodeVersionConflict,
			fmt.Sprintf("task #%d is at version %d, expected %d", task.Number, task.Version, expectedVersion), true,
			domain.Detail{Field: "expected_version", Code: domain.CodeVersionConflict, Message: fmt.Sprintf("current version %d", task.Version)})
	}
	return task, nil
}

// updateTaskRow applies setClause and bumps version and updated_at, guarded
// by the expected version.
func updateTaskRow(ctx context.Context, tx sqlite.Executor, id string, version int64, now time.Time, setClause string, args ...any) error {
	args = append(args, sqlite.FormatTime(now), id, version)
	result, err := tx.ExecContext(ctx, "UPDATE tasks SET "+setClause+", version = version + 1, updated_at = ? WHERE id = ? AND version = ?", args...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return domain.NewError(domain.CodeVersionConflict, "task version changed during update", true)
	}
	return nil
}

func bumpReadyVersion(ctx context.Context, tx sqlite.Executor) error {
	_, err := tx.ExecContext(ctx, "UPDATE board SET ready_version = ready_version + 1 WHERE singleton = 1")
	return err
}

// appendEvent records one audit event. task is nil for board-level events;
// otherwise task.Version must already be the post-mutation version.
func appendEvent(ctx context.Context, tx sqlite.Executor, task *domain.Task, eventType domain.EventType, actor string, payload any, now time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var taskID, version any
	if task != nil {
		taskID, version = task.ID, task.Version
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_events(task_id, type, actor, task_version, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, taskID, eventType, actor, version, string(encoded), sqlite.FormatTime(now))
	return err
}

func readySetMismatch() error {
	return domain.NewError(domain.CodeReadyOrderConflict, "tasks must be exactly the current READY queue members, each once", false)
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}
