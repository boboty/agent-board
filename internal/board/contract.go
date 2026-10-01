package board

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/boboty/agent-board/internal/domain"
)

// Every mutation input carries Actor, the free-form label of who requested it
// (recorded in the audit event, never used for authorization), and an
// optional IdempotencyKey. Replaying a key with an identical request returns
// the original result without a second mutation or event; replaying it with
// a different request fails with IDEMPOTENCY_CONFLICT.
//
// Task references accept a task ID (ULID) or a task number ("12" or "#12").

// CreateTaskInput creates a task in state READY, not yet queued.
type CreateTaskInput struct {
	Actor              string `json:"actor"`
	IdempotencyKey     string `json:"-"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
}

// ListTasksInput filters tasks. Empty filters match every task. Results are
// ordered by task number.
type ListTasksInput struct {
	States []domain.State `json:"states,omitempty"`
	Queued *bool          `json:"queued,omitempty"`
}

// UpdateTaskInput edits task content. Nil fields are left unchanged.
type UpdateTaskInput struct {
	Actor              string  `json:"actor"`
	IdempotencyKey     string  `json:"-"`
	Task               string  `json:"task"`
	ExpectedVersion    int64   `json:"expected_version"`
	Title              *string `json:"title,omitempty"`
	Description        *string `json:"description,omitempty"`
	AcceptanceCriteria *string `json:"acceptance_criteria,omitempty"`
}

// QueueTaskInput appends a task to the end of the READY ordering. Queueing
// does not change task state.
type QueueTaskInput struct {
	Actor           string `json:"actor"`
	IdempotencyKey  string `json:"-"`
	Task            string `json:"task"`
	ExpectedVersion int64  `json:"expected_version"`
}

// SetTaskStateInput records an explicit task-level state. Any state may be
// recorded from any state; Reason replaces the task's state reason (nil
// clears it).
type SetTaskStateInput struct {
	Actor           string       `json:"actor"`
	IdempotencyKey  string       `json:"-"`
	Task            string       `json:"task"`
	ExpectedVersion int64        `json:"expected_version"`
	State           domain.State `json:"state"`
	Reason          *string      `json:"reason,omitempty"`
}

// ReorderReadyInput replaces the READY ordering. Tasks must be exactly the
// current READY queue members in the desired order, and ExpectedVersion must
// equal the current ReadyQueue.Version.
type ReorderReadyInput struct {
	Actor           string   `json:"actor"`
	IdempotencyKey  string   `json:"-"`
	ExpectedVersion int64    `json:"expected_version"`
	Tasks           []string `json:"tasks"`
}

// RecordFactInput appends an immutable fact to a task. Data, when present,
// must be a JSON object; the Board stores it without interpreting it.
type RecordFactInput struct {
	Actor          string          `json:"actor"`
	IdempotencyKey string          `json:"-"`
	Task           string          `json:"task"`
	Kind           domain.FactKind `json:"kind"`
	Body           string          `json:"body"`
	Data           json.RawMessage `json:"data,omitempty"`
}

// ListFactsInput lists a task's facts in recording order, optionally by kind.
type ListFactsInput struct {
	Task string          `json:"task"`
	Kind domain.FactKind `json:"kind,omitempty"`
}

// ListEventsInput pages through the audit log in commit order. Task, when
// set, restricts the result to that task's events. AfterID is an exclusive
// cursor; Limit defaults to DefaultEventLimit and is capped at MaxEventLimit.
type ListEventsInput struct {
	Task    string `json:"task,omitempty"`
	AfterID int64  `json:"after_id,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// Event listing bounds.
const (
	DefaultEventLimit = 100
	MaxEventLimit     = 1000
)

func normalizeCommon(actor *string, key *string) error {
	normalized, err := domain.NormalizeActor(*actor)
	if err != nil {
		return err
	}
	*actor = normalized
	*key = strings.TrimSpace(*key)
	return nil
}

func requireTaskRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", domain.Invalid("task", "must not be empty")
	}
	return ref, nil
}

func requireVersion(version int64) error {
	if version < 1 {
		return domain.Invalid("expected_version", "must be a positive task version")
	}
	return nil
}

func (in *CreateTaskInput) normalize() error {
	if err := normalizeCommon(&in.Actor, &in.IdempotencyKey); err != nil {
		return err
	}
	title, err := domain.NormalizeTitle(in.Title)
	if err != nil {
		return err
	}
	in.Title = title
	return nil
}

func (in *UpdateTaskInput) normalize() error {
	if err := normalizeCommon(&in.Actor, &in.IdempotencyKey); err != nil {
		return err
	}
	ref, err := requireTaskRef(in.Task)
	if err != nil {
		return err
	}
	in.Task = ref
	if err := requireVersion(in.ExpectedVersion); err != nil {
		return err
	}
	if in.Title == nil && in.Description == nil && in.AcceptanceCriteria == nil {
		return domain.Invalid("fields", "at least one of title, description, acceptance_criteria is required")
	}
	if in.Title != nil {
		title, err := domain.NormalizeTitle(*in.Title)
		if err != nil {
			return err
		}
		in.Title = &title
	}
	return nil
}

func (in *QueueTaskInput) normalize() error {
	if err := normalizeCommon(&in.Actor, &in.IdempotencyKey); err != nil {
		return err
	}
	ref, err := requireTaskRef(in.Task)
	if err != nil {
		return err
	}
	in.Task = ref
	return requireVersion(in.ExpectedVersion)
}

func (in *SetTaskStateInput) normalize() error {
	if err := normalizeCommon(&in.Actor, &in.IdempotencyKey); err != nil {
		return err
	}
	ref, err := requireTaskRef(in.Task)
	if err != nil {
		return err
	}
	in.Task = ref
	if err := requireVersion(in.ExpectedVersion); err != nil {
		return err
	}
	if !in.State.Valid() {
		return domain.Invalid("state", "must be one of READY, IN_PROGRESS, DONE, BLOCKED")
	}
	if in.Reason != nil {
		reason := strings.TrimSpace(*in.Reason)
		if reason == "" {
			in.Reason = nil
		} else {
			in.Reason = &reason
		}
	}
	return nil
}

func (in *ReorderReadyInput) normalize() error {
	if err := normalizeCommon(&in.Actor, &in.IdempotencyKey); err != nil {
		return err
	}
	if in.ExpectedVersion < 1 {
		return domain.Invalid("expected_version", "must be a positive READY queue version")
	}
	for i, ref := range in.Tasks {
		ref, err := requireTaskRef(ref)
		if err != nil {
			return err
		}
		in.Tasks[i] = ref
	}
	return nil
}

func (in *RecordFactInput) normalize() error {
	if err := normalizeCommon(&in.Actor, &in.IdempotencyKey); err != nil {
		return err
	}
	ref, err := requireTaskRef(in.Task)
	if err != nil {
		return err
	}
	in.Task = ref
	if !in.Kind.Valid() {
		return domain.Invalid("kind", "must be one of execution, delivery, verification, handoff, note")
	}
	if strings.TrimSpace(in.Body) == "" {
		return domain.Invalid("body", "must not be empty")
	}
	if len(in.Data) > 0 {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(in.Data, &object); err != nil || object == nil {
			return domain.Invalid("data", "must be a JSON object")
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, in.Data); err != nil {
			return domain.Invalid("data", "must be a JSON object")
		}
		in.Data = compact.Bytes()
	}
	return nil
}
