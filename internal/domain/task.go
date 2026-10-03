// Package domain defines Agent Board's task ledger model: Task, TaskFact and
// TaskEvent. It records facts; it does not encode workflow policy.
package domain

import (
	"encoding/json"
	"strings"
	"time"
)

// State is an explicitly recorded task-level lifecycle state.
type State string

// The four task-level states. There are no others.
const (
	StateReady      State = "READY"
	StateInProgress State = "IN_PROGRESS"
	StateDone       State = "DONE"
	StateBlocked    State = "BLOCKED"
)

// States lists every valid State.
var States = []State{StateReady, StateInProgress, StateDone, StateBlocked}

// Valid reports whether s is one of the four task-level states.
func (s State) Valid() bool {
	switch s {
	case StateReady, StateInProgress, StateDone, StateBlocked:
		return true
	}
	return false
}

// FactKind classifies a TaskFact.
type FactKind string

// The v0 fact kinds.
const (
	FactExecution    FactKind = "execution"
	FactDelivery     FactKind = "delivery"
	FactVerification FactKind = "verification"
	FactDecision     FactKind = "decision"
	FactHandoff      FactKind = "handoff"
	FactNote         FactKind = "note"
)

// FactKinds lists every valid FactKind.
var FactKinds = []FactKind{FactExecution, FactDelivery, FactVerification, FactDecision, FactHandoff, FactNote}

// Valid reports whether k is a known fact kind.
func (k FactKind) Valid() bool {
	switch k {
	case FactExecution, FactDelivery, FactVerification, FactDecision, FactHandoff, FactNote:
		return true
	}
	return false
}

// Task is a schedulable, verifiable, hand-off-able unit of work.
//
// A task has no lifecycle state (State is nil) until it is queued; queueing
// explicitly records READY. After that, State changes only through an
// explicit set-state request and is always one of the four States. A task
// appears in the READY queue while it is queued and its State is READY.
type Task struct {
	ID                 string     `json:"id"`
	Number             int64      `json:"number"`
	Title              string     `json:"title"`
	Description        string     `json:"description"`
	AcceptanceCriteria string     `json:"acceptance_criteria"`
	State              *State     `json:"state"`
	StateReason        *string    `json:"state_reason,omitempty"`
	QueuedAt           *time.Time `json:"queued_at,omitempty"`
	ReadyRank          *int64     `json:"ready_rank,omitempty"`
	Version            int64      `json:"version"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// TaskFact is an immutable, append-only fact recorded against a task, such as
// execution metadata, delivery evidence, a verification result, a decision,
// or a handoff.
// Facts never change task state.
type TaskFact struct {
	ID             string          `json:"id"`
	TaskID         string          `json:"task_id"`
	Kind           FactKind        `json:"kind"`
	Body           string          `json:"body"`
	Data           json.RawMessage `json:"data,omitempty"`
	Baseline       *string         `json:"baseline,omitempty"`
	Fingerprint    *string         `json:"fingerprint,omitempty"`
	AcceptedCommit *string         `json:"accepted_commit,omitempty"`
	Verdict        *string         `json:"verdict,omitempty"`
	Provenance     *FactProvenance `json:"provenance"`
	Actor          string          `json:"actor"`
	CreatedAt      time.Time       `json:"created_at"`
}

// FactProvenance records optional, self-reported execution context. It is
// descriptive metadata only and is never used to authenticate or authorize.
type FactProvenance struct {
	Role    string `json:"role,omitempty"`
	Session string `json:"session,omitempty"`
	Harness string `json:"harness,omitempty"`
	Model   string `json:"model,omitempty"`
}

// EventType names an audit event.
type EventType string

// Audit event types; one per mutating operation.
const (
	EventTaskCreated    EventType = "task_created"
	EventTaskUpdated    EventType = "task_updated"
	EventTaskQueued     EventType = "task_queued"
	EventTaskStateSet   EventType = "task_state_set"
	EventReadyReordered EventType = "ready_reordered"
	EventFactRecorded   EventType = "fact_recorded"
)

// TaskEvent is one append-only audit record, committed in the same
// transaction as the mutation it describes. TaskID is nil for board-level
// events such as a READY reorder. TaskVersion is the task version after the
// mutation, when the event concerns one task.
type TaskEvent struct {
	ID          int64           `json:"id"`
	TaskID      *string         `json:"task_id,omitempty"`
	Type        EventType       `json:"type"`
	Actor       string          `json:"actor"`
	TaskVersion *int64          `json:"task_version,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   time.Time       `json:"created_at"`
}

// ReadyQueue is the ordered READY view: queued tasks whose state is READY,
// ordered by ReadyRank. Version changes whenever membership or order may have
// changed and guards ReorderReady against lost updates.
type ReadyQueue struct {
	Version int64  `json:"version"`
	Tasks   []Task `json:"tasks"`
}

// NormalizeTitle trims a title and rejects an empty one.
func NormalizeTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", Invalid("title", "must not be empty")
	}
	return title, nil
}

// NormalizeActor trims an actor label and rejects an empty one. The Board
// records who requested a mutation; it does not authorize the actor.
func NormalizeActor(actor string) (string, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "", Invalid("actor", "must not be empty")
	}
	return actor, nil
}
