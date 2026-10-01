package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/boboty/agent-board/internal/board"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ops"
)

const testProjectID = "01M3VN4DT676SGJ90T58JRB13R"

func connect(t *testing.T, defaultActor string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	service, err := board.Open(ctx, board.Config{DatabasePath: filepath.Join(t.TempDir(), "board.db"), ProjectID: testProjectID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close(ctx) })
	server := New(ops.New(service, defaultActor), Info{Version: "test", ProjectID: testProjectID, DatabasePath: "board.db"})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, arguments any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: protocol error %v", name, err)
	}
	return result
}

func decode[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var fromText T
	if err := json.Unmarshal([]byte(text), &fromText); err != nil {
		t.Fatalf("text content is not the same JSON: %v", err)
	}
	return value
}

type toolError struct {
	Error domain.Error `json:"error"`
}

func requireToolError(t *testing.T, result *mcp.CallToolResult, code string) domain.Error {
	t.Helper()
	if !result.IsError {
		t.Fatalf("expected %s, got success %v", code, result.StructuredContent)
	}
	got := decode[toolError](t, result).Error
	if got.Code != code {
		t.Fatalf("got %+v, want %s", got, code)
	}
	return got
}

// The tool list is generated from the ops catalog: the Board contract and
// nothing else (no role, workflow, or runtime tools).
func TestToolsAreTheOperationCatalog(t *testing.T) {
	session := connect(t, "tester")
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	readOnly := map[string]bool{}
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
		readOnly[tool.Name] = tool.Annotations.ReadOnlyHint
	}
	want := []string{"create_task", "get_task", "list_tasks", "update_task", "queue_task", "set_task_state",
		"list_ready", "reorder_ready", "record_fact", "list_facts", "list_events"}
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("tools %v", names)
	}
	for _, op := range ops.Operations() {
		if readOnly[op.Name] != op.ReadOnly {
			t.Errorf("%s readOnlyHint %v", op.Name, readOnly[op.Name])
		}
	}
}

func TestToolCallsReachTheBoard(t *testing.T) {
	session := connect(t, "mcp-default")
	created := decode[ops.TaskResult](t, callTool(t, session, "create_task", map[string]any{"title": "via mcp"})).Task
	if created.Number != 1 || created.State != nil {
		t.Fatalf("created %+v", created)
	}
	queued := decode[ops.TaskResult](t, callTool(t, session, "queue_task", map[string]any{"task": "#1", "expected_version": 1})).Task
	started := decode[ops.TaskResult](t, callTool(t, session, "set_task_state",
		map[string]any{"task": created.ID, "expected_version": queued.Version, "state": "IN_PROGRESS", "actor": "orchestrator"})).Task
	if *started.State != domain.StateInProgress {
		t.Fatalf("started %+v", started)
	}
	callTool(t, session, "record_fact", map[string]any{"task": "1", "kind": "delivery", "body": "done", "data": map[string]any{"commit": "abc"}})
	facts := decode[ops.FactsResult](t, callTool(t, session, "list_facts", map[string]any{"task": "1"})).Facts
	if len(facts) != 1 || string(facts[0].Data) != `{"commit":"abc"}` {
		t.Fatalf("facts %+v", facts)
	}
	// Recording a fact changes nothing about the task.
	after := decode[ops.TaskResult](t, callTool(t, session, "get_task", map[string]any{"task": "1"})).Task
	if after.Version != started.Version || *after.State != domain.StateInProgress {
		t.Fatalf("task after fact %+v", after)
	}
	events := decode[ops.EventsResult](t, callTool(t, session, "list_events", map[string]any{"task": "1"})).Events
	actors := []string{}
	for _, event := range events {
		actors = append(actors, event.Actor)
	}
	if !slices.Equal(actors, []string{"mcp-default", "mcp-default", "orchestrator", "mcp-default"}) {
		t.Fatalf("actors %v", actors)
	}
}

func TestToolErrorsAreStructured(t *testing.T) {
	session := connect(t, "tester")
	callTool(t, session, "create_task", map[string]any{"title": "a"})

	conflict := requireToolError(t, callTool(t, session, "queue_task", map[string]any{"task": "1", "expected_version": 5}), domain.CodeVersionConflict)
	if !conflict.Retryable || conflict.Details[0].Field != "expected_version" {
		t.Fatalf("conflict %+v", conflict)
	}
	requireToolError(t, callTool(t, session, "set_task_state", map[string]any{"task": "1", "expected_version": 1, "state": "DONE"}), domain.CodeTaskNotQueued)
	requireToolError(t, callTool(t, session, "get_task", map[string]any{"task": "99"}), domain.CodeTaskNotFound)
	requireToolError(t, callTool(t, session, "create_task", map[string]any{"title": "x", "priority": 1}), domain.CodeInvalidArgument)
	requireToolError(t, callTool(t, session, "get_task", map[string]any{"task": 1}), domain.CodeInvalidArgument)
	requireToolError(t, callTool(t, session, "record_fact", map[string]any{"task": "1", "kind": "review", "body": "x"}), domain.CodeInvalidArgument)

	noActor := connect(t, "")
	invalid := requireToolError(t, callTool(t, noActor, "create_task", map[string]any{"title": "x"}), domain.CodeInvalidArgument)
	if invalid.Details[0].Field != "actor" {
		t.Fatalf("no actor %+v", invalid)
	}

	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "claim_task", Arguments: map[string]any{}}); err == nil {
		t.Fatal("unknown tool did not fail")
	}
}
