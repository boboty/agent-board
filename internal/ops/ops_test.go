package ops

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/boboty/agent-board/internal/board"
	"github.com/boboty/agent-board/internal/domain"
)

const testProjectID = "01M3VN4DT676SGJ90T58JRB13R"

func openBoard(t *testing.T) *board.Service {
	t.Helper()
	ctx := context.Background()
	service, err := board.Open(ctx, board.Config{DatabasePath: filepath.Join(t.TempDir(), "board.db"), ProjectID: testProjectID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close(ctx) })
	return service
}

func call(t *testing.T, s *Service, name string, arguments string) (any, error) {
	t.Helper()
	return s.Call(context.Background(), name, json.RawMessage(arguments))
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	if !domain.IsCode(err, code) {
		t.Fatalf("got error %v, want %s", err, code)
	}
}

func camel(snake string) string {
	var b strings.Builder
	for _, part := range strings.Split(snake, "_") {
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	return b.String()
}

// The catalog is exactly the Board contract: one operation per board.Service
// method, and nothing else.
func TestOperationsAreExactlyTheBoardContract(t *testing.T) {
	var methods []string
	serviceType := reflect.TypeFor[*board.Service]()
	for i := range serviceType.NumMethod() {
		if name := serviceType.Method(i).Name; name != "Close" {
			methods = append(methods, name)
		}
	}
	var operations []string
	for _, op := range Operations() {
		operations = append(operations, camel(op.Name))
		if _, ok := reflect.TypeFor[*Service]().MethodByName(camel(op.Name)); !ok {
			t.Errorf("operation %s has no ops.Service method", op.Name)
		}
	}
	slices.Sort(methods)
	slices.Sort(operations)
	if !slices.Equal(methods, operations) {
		t.Fatalf("board methods %v, operations %v", methods, operations)
	}
}

func TestInputSchemas(t *testing.T) {
	schemas := map[string]Operation{}
	for _, op := range Operations() {
		if op.InputSchema == nil || op.InputSchema.Type != "object" {
			t.Fatalf("%s: input schema %+v", op.Name, op.InputSchema)
		}
		schemas[op.Name] = op
	}
	if got := schemas["create_task"].InputSchema.Required; !slices.Equal(got, []string{"title"}) {
		t.Errorf("create_task required %v", got)
	}
	if got := schemas["set_task_state"].InputSchema.Required; !slices.Equal(got, []string{"task", "expected_version", "state"}) {
		t.Errorf("set_task_state required %v", got)
	}
	enum := schemas["set_task_state"].InputSchema.Properties["state"].Enum
	if len(enum) != len(domain.States) {
		t.Errorf("state enum %v", enum)
	}
	if typ := schemas["record_fact"].InputSchema.Properties["data"].Type; typ != "object" {
		t.Errorf("record_fact data type %q", typ)
	}
	for _, field := range []string{"baseline", "fingerprint", "accepted_commit", "verdict"} {
		encoded, _ := json.Marshal(schemas["record_fact"].InputSchema.Properties[field])
		if !strings.Contains(string(encoded), `"string"`) {
			t.Errorf("record_fact %s schema %s", field, encoded)
		}
	}
	kinds := schemas["record_fact"].InputSchema.Properties["kind"].Enum
	if len(kinds) != len(domain.FactKinds) || !slices.Contains(kinds, any(string(domain.FactDecision))) {
		t.Errorf("record_fact kind enum %v", kinds)
	}
}

func TestCallRejectsMalformedArguments(t *testing.T) {
	s := New(openBoard(t), "tester")
	for _, test := range []struct{ name, op, arguments string }{
		{"unknown field", "create_task", `{"title":"x","expectedVersion":1}`},
		{"wrong type", "get_task", `{"task":12}`},
		{"trailing data", "list_ready", `{} {}`},
		{"not an object", "list_tasks", `[]`},
		{"wrong core field type", "record_fact", `{"task":"1","kind":"delivery","body":"x","baseline":false,"fingerprint":"hash"}`},
		{"unknown operation", "start_task", `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := call(t, s, test.op, test.arguments)
			requireCode(t, err, domain.CodeInvalidArgument)
		})
	}
	if _, err := call(t, s, "list_ready", ``); err != nil {
		t.Fatalf("empty arguments: %v", err)
	}
}

// Validation belongs to the Board; ops passes its errors through unchanged.
func TestBoardErrorsPassThrough(t *testing.T) {
	s := New(openBoard(t), "tester")
	_, err := call(t, s, "create_task", `{"title":"  "}`)
	requireCode(t, err, domain.CodeInvalidArgument)
	if described := DescribeError(err); described.Details[0].Field != "title" {
		t.Fatalf("details %+v", described.Details)
	}
	_, err = call(t, s, "get_task", `{"task":"#9"}`)
	requireCode(t, err, domain.CodeTaskNotFound)

	if _, err := call(t, s, "create_task", `{"title":"a"}`); err != nil {
		t.Fatal(err)
	}
	_, err = call(t, s, "set_task_state", `{"task":"1","expected_version":1,"state":"DONE"}`)
	requireCode(t, err, domain.CodeTaskNotQueued)
	_, err = call(t, s, "set_task_state", `{"task":"1","expected_version":1,"state":"REVIEW"}`)
	requireCode(t, err, domain.CodeInvalidArgument)
	_, err = call(t, s, "queue_task", `{"task":"1","expected_version":7}`)
	requireCode(t, err, domain.CodeVersionConflict)
	if !DescribeError(err).Retryable {
		t.Fatal("VERSION_CONFLICT lost its retryable flag")
	}
}

func TestDefaultActor(t *testing.T) {
	b := openBoard(t)
	ctx := context.Background()
	if _, err := New(b, "").CreateTask(ctx, CreateTaskArgs{Title: "x"}); !domain.IsCode(err, domain.CodeInvalidArgument) {
		t.Fatalf("no actor: %v", err)
	}
	s := New(b, "default-actor")
	if _, err := s.CreateTask(ctx, CreateTaskArgs{Title: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(ctx, CreateTaskArgs{Write: Write{Actor: "explicit"}, Title: "b"}); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListEvents(ctx, ListEventsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if events.Events[0].Actor != "default-actor" || events.Events[1].Actor != "explicit" {
		t.Fatalf("actors %q %q", events.Events[0].Actor, events.Events[1].Actor)
	}
}

func TestIdempotencyKeyReachesTheBoard(t *testing.T) {
	s := New(openBoard(t), "tester")
	first, err := call(t, s, "create_task", `{"title":"once","idempotency_key":"k1"}`)
	if err != nil {
		t.Fatal(err)
	}
	again, err := call(t, s, "create_task", `{"title":"once","idempotency_key":"k1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if first.(TaskResult).Task.ID != again.(TaskResult).Task.ID {
		t.Fatal("replay created a second task")
	}
	_, err = call(t, s, "create_task", `{"title":"different","idempotency_key":"k1"}`)
	requireCode(t, err, domain.CodeIdempotencyConflict)
	tasks, err := call(t, s, "list_tasks", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(tasks.(TasksResult).Tasks); n != 1 {
		t.Fatalf("%d tasks", n)
	}
}

func TestCallRunsEveryOperation(t *testing.T) {
	s := New(openBoard(t), "tester")
	steps := []struct{ op, arguments string }{
		{"create_task", `{"title":"a","description":"d","acceptance_criteria":"ac"}`},
		{"create_task", `{"title":"b"}`},
		{"get_task", `{"task":"#1"}`},
		{"update_task", `{"task":"1","expected_version":1,"title":"a2"}`},
		{"queue_task", `{"task":"1","expected_version":2}`},
		{"queue_task", `{"task":"2","expected_version":1}`},
		{"list_ready", `{}`},
		{"reorder_ready", `{"expected_version":3,"tasks":["2","1"]}`},
		{"set_task_state", `{"task":"1","expected_version":3,"state":"BLOCKED","reason":"waiting"}`},
		{"record_fact", `{"task":"1","kind":"delivery","body":"context","data":{"branch":"x"},"baseline":"base","fingerprint":"fingerprint","provenance":{"role":"worker","session":"ops-session","harness":"codex","model":"gpt-6"}}`},
		{"list_facts", `{"task":"1","kind":"delivery"}`},
		{"list_tasks", `{"states":["BLOCKED"],"queued":true}`},
		{"list_events", `{"task":"1","limit":2}`},
	}
	covered := map[string]bool{}
	for _, step := range steps {
		if _, err := call(t, s, step.op, step.arguments); err != nil {
			t.Fatalf("%s %s: %v", step.op, step.arguments, err)
		}
		covered[step.op] = true
	}
	for _, op := range Operations() {
		if !covered[op.Name] {
			t.Errorf("operation %s not exercised", op.Name)
		}
	}
	facts, err := call(t, s, "list_facts", `{"task":"1","kind":"delivery"}`)
	if err != nil {
		t.Fatal(err)
	}
	listed := facts.(FactsResult).Facts
	if len(listed) != 1 || listed[0].Provenance == nil || listed[0].Provenance.Role != "worker" || listed[0].Provenance.Session != "ops-session" {
		t.Fatalf("fact provenance through ops API: %+v", listed)
	}
	if listed[0].Baseline == nil || *listed[0].Baseline != "base" || listed[0].Fingerprint == nil || *listed[0].Fingerprint != "fingerprint" {
		t.Fatalf("structured delivery semantics through ops API: %+v", listed)
	}
	task, err := call(t, s, "get_task", `{"task":"1"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := task.(TaskResult).Task
	if *got.State != domain.StateBlocked || *got.StateReason != "waiting" || got.Title != "a2" {
		t.Fatalf("task %+v", got)
	}
}

func TestDescribeError(t *testing.T) {
	if got := DescribeError(context.Canceled); got.Code != CodeCanceled {
		t.Fatalf("canceled: %+v", got)
	}
	if got := DescribeError(errString("boom")); got.Code != CodeInternal || got.Message != "boom" {
		t.Fatalf("internal: %+v", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
