// Package ops is the transport-neutral operation surface shared by every
// Board adapter. The MCP server and the CLI both call these operations; they
// add only transport (argument decoding, output encoding) and never validate,
// infer, or decide anything themselves.
//
// Each operation maps one-to-one onto a board.Service method. Arguments are
// copied into the Board's contract input unchanged, so validation,
// normalization, version checks, idempotency, and transactions all happen in
// the Board. The only adapter-level behavior is the default actor, a label
// configured when the adapter starts and used when a request names none.
//
// Operations encode no Workflow Skill rules: no roles, no permissions, no
// automatic transitions, no state inferred from facts or runtime activity.
package ops

import (
	"context"
	"encoding/json"

	"github.com/boboty/agent-board/internal/board"
	"github.com/boboty/agent-board/internal/domain"
)

// Service exposes the Board operations to adapters.
type Service struct {
	board        *board.Service
	defaultActor string
}

// New wraps an open Board. defaultActor, when non-empty, is recorded for
// mutations whose request leaves actor empty.
func New(b *board.Service, defaultActor string) *Service {
	return &Service{board: b, defaultActor: defaultActor}
}

// Write carries the fields every mutation accepts.
type Write struct {
	Actor          string `json:"actor,omitempty" jsonschema:"Free-form label of who requests the change, recorded in the audit log and never used for authorization. Optional only when the server was started with a default actor."`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"Optional retry key. Replaying it with an identical request returns the original result without a second change; reusing it with a different request fails with IDEMPOTENCY_CONFLICT."`
}

func (s *Service) actor(requested string) string {
	if requested == "" {
		return s.defaultActor
	}
	return requested
}

// TaskResult wraps one task.
type TaskResult struct {
	Task domain.Task `json:"task"`
}

// TasksResult wraps a task list.
type TasksResult struct {
	Tasks []domain.Task `json:"tasks"`
}

// FactResult wraps one fact.
type FactResult struct {
	Fact domain.TaskFact `json:"fact"`
}

// FactsResult wraps a fact list.
type FactsResult struct {
	Facts []domain.TaskFact `json:"facts"`
}

// EventsResult wraps an audit event page.
type EventsResult struct {
	Events []domain.TaskEvent `json:"events"`
}

// CreateTaskArgs creates an unqueued task.
type CreateTaskArgs struct {
	Write
	Title              string `json:"title" jsonschema:"Task title; must not be blank."`
	Description        string `json:"description,omitempty" jsonschema:"Task description."`
	AcceptanceCriteria string `json:"acceptance_criteria,omitempty" jsonschema:"Acceptance criteria used to verify the task."`
}

// CreateTask creates an unqueued task with no lifecycle state.
func (s *Service) CreateTask(ctx context.Context, args CreateTaskArgs) (TaskResult, error) {
	task, err := s.board.CreateTask(ctx, board.CreateTaskInput{
		Actor:              s.actor(args.Actor),
		IdempotencyKey:     args.IdempotencyKey,
		Title:              args.Title,
		Description:        args.Description,
		AcceptanceCriteria: args.AcceptanceCriteria,
	})
	return TaskResult{task}, err
}

// GetTaskArgs selects one task.
type GetTaskArgs struct {
	Task string `json:"task" jsonschema:"Task ID (ULID) or task number such as 12 or #12."`
}

// GetTask returns one task.
func (s *Service) GetTask(ctx context.Context, args GetTaskArgs) (TaskResult, error) {
	task, err := s.board.GetTask(ctx, args.Task)
	return TaskResult{task}, err
}

// ListTasksArgs filters tasks.
type ListTasksArgs struct {
	States []domain.State `json:"states,omitempty" jsonschema:"Only tasks whose recorded state is one of these. Unqueued tasks have no state and never match."`
	Queued *bool          `json:"queued,omitempty" jsonschema:"true: only queued tasks; false: only unqueued tasks; omitted: both."`
}

// ListTasks returns matching tasks ordered by number.
func (s *Service) ListTasks(ctx context.Context, args ListTasksArgs) (TasksResult, error) {
	tasks, err := s.board.ListTasks(ctx, board.ListTasksInput{States: args.States, Queued: args.Queued})
	return TasksResult{tasks}, err
}

// UpdateTaskArgs edits task content.
type UpdateTaskArgs struct {
	Write
	Task               string  `json:"task" jsonschema:"Task ID (ULID) or task number."`
	ExpectedVersion    int64   `json:"expected_version" jsonschema:"The task version you last read; the change fails with VERSION_CONFLICT if the task has changed since."`
	Title              *string `json:"title,omitempty" jsonschema:"New title; omit to keep."`
	Description        *string `json:"description,omitempty" jsonschema:"New description; omit to keep."`
	AcceptanceCriteria *string `json:"acceptance_criteria,omitempty" jsonschema:"New acceptance criteria; omit to keep."`
}

// UpdateTask edits title, description, or acceptance criteria.
func (s *Service) UpdateTask(ctx context.Context, args UpdateTaskArgs) (TaskResult, error) {
	task, err := s.board.UpdateTask(ctx, board.UpdateTaskInput{
		Actor:              s.actor(args.Actor),
		IdempotencyKey:     args.IdempotencyKey,
		Task:               args.Task,
		ExpectedVersion:    args.ExpectedVersion,
		Title:              args.Title,
		Description:        args.Description,
		AcceptanceCriteria: args.AcceptanceCriteria,
	})
	return TaskResult{task}, err
}

// QueueTaskArgs queues an unqueued task.
type QueueTaskArgs struct {
	Write
	Task            string `json:"task" jsonschema:"Task ID (ULID) or task number."`
	ExpectedVersion int64  `json:"expected_version" jsonschema:"The task version you last read."`
}

// QueueTask records state READY and appends the task to the READY ordering.
func (s *Service) QueueTask(ctx context.Context, args QueueTaskArgs) (TaskResult, error) {
	task, err := s.board.QueueTask(ctx, board.QueueTaskInput{
		Actor:           s.actor(args.Actor),
		IdempotencyKey:  args.IdempotencyKey,
		Task:            args.Task,
		ExpectedVersion: args.ExpectedVersion,
	})
	return TaskResult{task}, err
}

// SetTaskStateArgs records an explicit task-level state.
type SetTaskStateArgs struct {
	Write
	Task            string       `json:"task" jsonschema:"Task ID (ULID) or task number."`
	ExpectedVersion int64        `json:"expected_version" jsonschema:"The task version you last read."`
	State           domain.State `json:"state" jsonschema:"The task-level state to record."`
	Reason          *string      `json:"reason,omitempty" jsonschema:"Replaces the task's state reason, such as why it is BLOCKED; omit to clear it."`
}

// SetTaskState records the requested state on a queued task.
func (s *Service) SetTaskState(ctx context.Context, args SetTaskStateArgs) (TaskResult, error) {
	task, err := s.board.SetTaskState(ctx, board.SetTaskStateInput{
		Actor:           s.actor(args.Actor),
		IdempotencyKey:  args.IdempotencyKey,
		Task:            args.Task,
		ExpectedVersion: args.ExpectedVersion,
		State:           args.State,
		Reason:          args.Reason,
	})
	return TaskResult{task}, err
}

// ListReadyArgs takes no arguments.
type ListReadyArgs struct{}

// ListReady returns the READY queue in order with its version.
func (s *Service) ListReady(ctx context.Context, _ ListReadyArgs) (domain.ReadyQueue, error) {
	return s.board.ListReady(ctx)
}

// ReorderReadyArgs replaces the READY ordering.
type ReorderReadyArgs struct {
	Write
	ExpectedVersion int64    `json:"expected_version" jsonschema:"The READY queue version from list_ready; fails with READY_ORDER_CONFLICT if the queue has changed since."`
	Tasks           []string `json:"tasks" jsonschema:"Exactly the current READY tasks (IDs or numbers), each once, in the desired order."`
}

// ReorderReady atomically replaces the READY order.
func (s *Service) ReorderReady(ctx context.Context, args ReorderReadyArgs) (domain.ReadyQueue, error) {
	return s.board.ReorderReady(ctx, board.ReorderReadyInput{
		Actor:           s.actor(args.Actor),
		IdempotencyKey:  args.IdempotencyKey,
		ExpectedVersion: args.ExpectedVersion,
		Tasks:           args.Tasks,
	})
}

// RecordFactArgs appends a fact.
type RecordFactArgs struct {
	Write
	Task string          `json:"task" jsonschema:"Task ID (ULID) or task number."`
	Kind domain.FactKind `json:"kind" jsonschema:"Fact kind."`
	Body string          `json:"body" jsonschema:"Human-readable fact content; must not be blank."`
	Data json.RawMessage `json:"data,omitempty" jsonschema:"Optional JSON object stored verbatim; the Board does not interpret it."`
}

// RecordFact appends an immutable fact. It never changes task state.
func (s *Service) RecordFact(ctx context.Context, args RecordFactArgs) (FactResult, error) {
	fact, err := s.board.RecordFact(ctx, board.RecordFactInput{
		Actor:          s.actor(args.Actor),
		IdempotencyKey: args.IdempotencyKey,
		Task:           args.Task,
		Kind:           args.Kind,
		Body:           args.Body,
		Data:           args.Data,
	})
	return FactResult{fact}, err
}

// ListFactsArgs selects a task's facts.
type ListFactsArgs struct {
	Task string          `json:"task" jsonschema:"Task ID (ULID) or task number."`
	Kind domain.FactKind `json:"kind,omitempty" jsonschema:"Only facts of this kind."`
}

// ListFacts returns a task's facts in recording order.
func (s *Service) ListFacts(ctx context.Context, args ListFactsArgs) (FactsResult, error) {
	facts, err := s.board.ListFacts(ctx, board.ListFactsInput{Task: args.Task, Kind: args.Kind})
	return FactsResult{facts}, err
}

// ListEventsArgs pages through the audit log.
type ListEventsArgs struct {
	Task    string `json:"task,omitempty" jsonschema:"Only this task's events (ID or number)."`
	AfterID int64  `json:"after_id,omitempty" jsonschema:"Exclusive cursor: return events with a larger id. Pass the last id of the previous page."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Page size; default 100, maximum 1000."`
}

// ListEvents returns audit events in commit order.
func (s *Service) ListEvents(ctx context.Context, args ListEventsArgs) (EventsResult, error) {
	events, err := s.board.ListEvents(ctx, board.ListEventsInput{Task: args.Task, AfterID: args.AfterID, Limit: args.Limit})
	return EventsResult{events}, err
}
