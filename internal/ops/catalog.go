package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/boboty/agent-board/internal/domain"
)

// Operation describes one Board operation for adapters that dispatch by name.
type Operation struct {
	Name        string
	Description string
	ReadOnly    bool
	// InputSchema is the JSON Schema of the operation's arguments object.
	InputSchema *jsonschema.Schema

	call func(context.Context, *Service, json.RawMessage) (any, error)
}

var catalog = []Operation{
	define("create_task", false, (*Service).CreateTask,
		"Create a task. A new task is unqueued and has no lifecycle state until queue_task."),
	define("get_task", true, (*Service).GetTask,
		"Get one task by ID or number."),
	define("list_tasks", true, (*Service).ListTasks,
		"List tasks ordered by number, optionally filtered by recorded state or queued status."),
	define("update_task", false, (*Service).UpdateTask,
		"Edit a task's title, description, or acceptance criteria. Requires the expected task version."),
	define("queue_task", false, (*Service).QueueTask,
		"Queue an unqueued task: records state READY and appends it to the end of the READY ordering."),
	define("set_task_state", false, (*Service).SetTaskState,
		"Record an explicit task-level state (READY, IN_PROGRESS, DONE, BLOCKED) on a queued task. The Board records the request as given; it applies no transition policy."),
	define("list_ready", true, (*Service).ListReady,
		"List the READY queue in order together with its version, which reorder_ready requires."),
	define("reorder_ready", false, (*Service).ReorderReady,
		"Atomically replace the READY ordering. Requires the READY queue version and exactly the current READY tasks."),
	define("record_fact", false, (*Service).RecordFact,
		"Append an immutable fact (execution, delivery, verification, handoff, note) to a task. Facts never change task state."),
	define("list_facts", true, (*Service).ListFacts,
		"List a task's facts in recording order, optionally by kind."),
	define("list_events", true, (*Service).ListEvents,
		"Page through the audit log in commit order, optionally for one task."),
}

// Operations returns the operation catalog in a stable order.
func Operations() []Operation {
	return append([]Operation(nil), catalog...)
}

// Call decodes arguments as JSON and runs the named operation. Unknown
// argument fields and type mismatches fail with INVALID_ARGUMENT; everything
// else is validated by the Board.
func (s *Service) Call(ctx context.Context, name string, arguments json.RawMessage) (any, error) {
	for _, op := range catalog {
		if op.Name == name {
			return op.call(ctx, s, arguments)
		}
	}
	return nil, domain.Invalid("operation", fmt.Sprintf("unknown operation %q", name))
}

func define[A, R any](name string, readOnly bool, method func(*Service, context.Context, A) (R, error), description string) Operation {
	schema, err := jsonschema.For[A](&jsonschema.ForOptions{TypeSchemas: typeSchemas()})
	if err != nil {
		panic(err)
	}
	return Operation{
		Name:        name,
		Description: description,
		ReadOnly:    readOnly,
		InputSchema: schema,
		call: func(ctx context.Context, s *Service, arguments json.RawMessage) (any, error) {
			var args A
			if err := decodeArguments(arguments, &args); err != nil {
				return nil, err
			}
			return method(s, ctx, args)
		},
	}
}

func decodeArguments(arguments json.RawMessage, target any) error {
	if len(bytes.TrimSpace(arguments)) == 0 || bytes.Equal(bytes.TrimSpace(arguments), []byte("null")) {
		arguments = json.RawMessage("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return domain.Invalid("arguments", err.Error())
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return domain.Invalid("arguments", "trailing data after arguments object")
	}
	return nil
}

// typeSchemas advertises the domain's own value sets. They describe the
// arguments to callers; the Board still performs all validation.
func typeSchemas() map[reflect.Type]*jsonschema.Schema {
	states := make([]any, len(domain.States))
	for i, state := range domain.States {
		states[i] = string(state)
	}
	kinds := make([]any, len(domain.FactKinds))
	for i, kind := range domain.FactKinds {
		kinds[i] = string(kind)
	}
	return map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[domain.State]():    {Type: "string", Enum: states},
		reflect.TypeFor[domain.FactKind](): {Type: "string", Enum: kinds},
		reflect.TypeFor[json.RawMessage](): {Type: "object"},
	}
}

// Adapter-level error codes for failures that carry no Board error code.
const (
	// CodeCanceled means the request was canceled or timed out before completing.
	CodeCanceled = "CANCELED"
	// CodeInternal means an unexpected failure; Message carries its text.
	CodeInternal = "INTERNAL"
)

// DescribeError returns the structured error adapters report for err: the
// Board's own error unchanged when there is one, otherwise CANCELED or
// INTERNAL with the original message, so no failure is reported without a
// code and none is swallowed.
func DescribeError(err error) *domain.Error {
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		return domain.NewError(domainErr.Code, domainErr.Message, domainErr.Retryable, domainErr.Details...)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return domain.NewError(CodeCanceled, err.Error(), true)
	}
	return domain.NewError(CodeInternal, err.Error(), false)
}
