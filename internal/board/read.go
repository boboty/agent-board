package board

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ids"
	"github.com/boboty/agent-board/internal/sqlite"
)

// GetTask returns one task by ID or number.
func (s *Service) GetTask(ctx context.Context, ref string) (domain.Task, error) {
	ref, err := requireTaskRef(ref)
	if err != nil {
		return domain.Task{}, err
	}
	var task domain.Task
	err = s.db.Read(ctx, func(ctx context.Context, q sqlite.Queryer) error {
		id, err := resolveTaskID(ctx, q, ref)
		if err != nil {
			return err
		}
		task, err = loadTask(ctx, q, id)
		return err
	})
	return task, err
}

// ListTasks returns tasks matching the filter, ordered by number.
func (s *Service) ListTasks(ctx context.Context, in ListTasksInput) ([]domain.Task, error) {
	var where []string
	var args []any
	if len(in.States) > 0 {
		placeholders := make([]string, len(in.States))
		for i, state := range in.States {
			if !state.Valid() {
				return nil, domain.Invalid("states", "must contain only READY, IN_PROGRESS, DONE, BLOCKED")
			}
			placeholders[i] = "?"
			args = append(args, state)
		}
		where = append(where, "state IN ("+strings.Join(placeholders, ", ")+")")
	}
	if in.Queued != nil {
		if *in.Queued {
			where = append(where, "queued_at IS NOT NULL")
		} else {
			where = append(where, "queued_at IS NULL")
		}
	}
	query := taskSelect
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY number"
	var tasks []domain.Task
	err := s.db.Read(ctx, func(ctx context.Context, q sqlite.Queryer) error {
		var err error
		tasks, err = queryTasks(ctx, q, query, args...)
		return err
	})
	return tasks, err
}

// ListReady returns the READY queue in order with its current version.
func (s *Service) ListReady(ctx context.Context) (domain.ReadyQueue, error) {
	var queue domain.ReadyQueue
	err := s.db.Read(ctx, func(ctx context.Context, q sqlite.Queryer) error {
		var err error
		queue, err = loadReadyQueue(ctx, q)
		return err
	})
	return queue, err
}

// ListFacts returns a task's facts in recording order.
func (s *Service) ListFacts(ctx context.Context, in ListFactsInput) ([]domain.TaskFact, error) {
	ref, err := requireTaskRef(in.Task)
	if err != nil {
		return nil, err
	}
	if in.Kind != "" && !in.Kind.Valid() {
		return nil, domain.Invalid("kind", "must be one of execution, delivery, verification, handoff, note")
	}
	facts := []domain.TaskFact{}
	err = s.db.Read(ctx, func(ctx context.Context, q sqlite.Queryer) error {
		id, err := resolveTaskID(ctx, q, ref)
		if err != nil {
			return err
		}
		rows, err := q.QueryContext(ctx, `SELECT id, task_id, kind, body, data, actor, created_at FROM task_facts
			WHERE task_id = ? AND (? = '' OR kind = ?) ORDER BY created_at, id`, id, in.Kind, in.Kind)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var fact domain.TaskFact
			var data sql.NullString
			var createdAt string
			if err := rows.Scan(&fact.ID, &fact.TaskID, &fact.Kind, &fact.Body, &data, &fact.Actor, &createdAt); err != nil {
				return err
			}
			if data.Valid {
				fact.Data = json.RawMessage(data.String)
			}
			if fact.CreatedAt, err = sqlite.ParseTime(createdAt); err != nil {
				return corrupt(err)
			}
			facts = append(facts, fact)
		}
		return rows.Err()
	})
	return facts, err
}

// ListEvents pages through the audit log in commit order.
func (s *Service) ListEvents(ctx context.Context, in ListEventsInput) ([]domain.TaskEvent, error) {
	limit := in.Limit
	switch {
	case limit < 0 || limit > MaxEventLimit:
		return nil, domain.Invalid("limit", "must be between 0 and "+strconv.Itoa(MaxEventLimit))
	case limit == 0:
		limit = DefaultEventLimit
	}
	if in.AfterID < 0 {
		return nil, domain.Invalid("after_id", "must not be negative")
	}
	ref := strings.TrimSpace(in.Task)
	events := []domain.TaskEvent{}
	err := s.db.Read(ctx, func(ctx context.Context, q sqlite.Queryer) error {
		query := `SELECT id, task_id, type, actor, task_version, payload, created_at FROM task_events WHERE id > ?`
		args := []any{in.AfterID}
		if ref != "" {
			id, err := resolveTaskID(ctx, q, ref)
			if err != nil {
				return err
			}
			query += " AND task_id = ?"
			args = append(args, id)
		}
		query += " ORDER BY id LIMIT ?"
		args = append(args, limit)
		rows, err := q.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var event domain.TaskEvent
			var taskID sql.NullString
			var version sql.NullInt64
			var payload, createdAt string
			if err := rows.Scan(&event.ID, &taskID, &event.Type, &event.Actor, &version, &payload, &createdAt); err != nil {
				return err
			}
			if taskID.Valid {
				event.TaskID = &taskID.String
			}
			if version.Valid {
				event.TaskVersion = &version.Int64
			}
			event.Payload = json.RawMessage(payload)
			if event.CreatedAt, err = sqlite.ParseTime(createdAt); err != nil {
				return corrupt(err)
			}
			events = append(events, event)
		}
		return rows.Err()
	})
	return events, err
}

const taskSelect = `SELECT id, number, title, description, acceptance_criteria, state, state_reason,
	queued_at, ready_rank, version, created_at, updated_at FROM tasks`

// resolveTaskID maps a task reference (ULID, "12", or "#12") to a task ID.
func resolveTaskID(ctx context.Context, q sqlite.Queryer, ref string) (string, error) {
	var row *sql.Row
	if _, err := ids.ParseStrict(ref); err == nil {
		row = q.QueryRowContext(ctx, "SELECT id FROM tasks WHERE id = ?", ref)
	} else if number, err := strconv.ParseInt(strings.TrimPrefix(ref, "#"), 10, 64); err == nil && number > 0 {
		row = q.QueryRowContext(ctx, "SELECT id FROM tasks WHERE number = ?", number)
	} else {
		return "", domain.Invalid("task", "must be a task ID or task number")
	}
	var id string
	if err := row.Scan(&id); err != nil {
		if isNoRows(err) {
			return "", domain.NewError(domain.CodeTaskNotFound, "task "+ref+" not found", false)
		}
		return "", err
	}
	return id, nil
}

func loadTask(ctx context.Context, q sqlite.Queryer, id string) (domain.Task, error) {
	tasks, err := queryTasks(ctx, q, taskSelect+" WHERE id = ?", id)
	if err != nil {
		return domain.Task{}, err
	}
	if len(tasks) == 0 {
		return domain.Task{}, domain.NewError(domain.CodeTaskNotFound, "task "+id+" not found", false)
	}
	return tasks[0], nil
}

func loadReadyQueue(ctx context.Context, q sqlite.Queryer) (domain.ReadyQueue, error) {
	var queue domain.ReadyQueue
	if err := q.QueryRowContext(ctx, "SELECT ready_version FROM board WHERE singleton = 1").Scan(&queue.Version); err != nil {
		return domain.ReadyQueue{}, err
	}
	tasks, err := queryTasks(ctx, q, taskSelect+" WHERE state = 'READY' AND queued_at IS NOT NULL ORDER BY ready_rank")
	if err != nil {
		return domain.ReadyQueue{}, err
	}
	queue.Tasks = tasks
	return queue, nil
}

func queryTasks(ctx context.Context, q sqlite.Queryer, query string, args ...any) ([]domain.Task, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []domain.Task{}
	for rows.Next() {
		var task domain.Task
		var reason, queuedAt sql.NullString
		var rank sql.NullInt64
		var createdAt, updatedAt string
		if err := rows.Scan(&task.ID, &task.Number, &task.Title, &task.Description, &task.AcceptanceCriteria,
			&task.State, &reason, &queuedAt, &rank, &task.Version, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if reason.Valid {
			task.StateReason = &reason.String
		}
		if queuedAt.Valid {
			parsed, err := sqlite.ParseTime(queuedAt.String)
			if err != nil {
				return nil, corrupt(err)
			}
			task.QueuedAt = &parsed
		}
		if rank.Valid {
			task.ReadyRank = &rank.Int64
		}
		if task.CreatedAt, err = sqlite.ParseTime(createdAt); err != nil {
			return nil, corrupt(err)
		}
		if task.UpdatedAt, err = sqlite.ParseTime(updatedAt); err != nil {
			return nil, corrupt(err)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func corrupt(err error) error {
	return domain.WrapError(err, domain.CodeStorageCorrupt, "stored record is invalid", false)
}
