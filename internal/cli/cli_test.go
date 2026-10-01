package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ops"
	"github.com/boboty/agent-board/internal/projectconfig"
	"github.com/boboty/agent-board/internal/workspace"
)

type harness struct {
	t    *testing.T
	repo string
	env  Env
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	home := t.TempDir()
	repo := t.TempDir()
	h := &harness{t: t, repo: repo, env: Env{
		Dir:     repo,
		Paths:   projectconfig.PathInputs{GOOS: runtime.GOOS, HomeDir: home, XDGDataHome: filepath.Join(home, "xdg"), LocalAppData: home},
		Actor:   "cli-test",
		Version: "test",
	}}
	h.ok("init")
	return h
}

func (h *harness) run(stdin string, args ...string) (int, string, string) {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	env := h.env
	env.Stdin, env.Stdout, env.Stderr = strings.NewReader(stdin), &stdout, &stderr
	code := Run(context.Background(), args, env)
	return code, stdout.String(), stderr.String()
}

func (h *harness) ok(args ...string) string {
	h.t.Helper()
	code, stdout, stderr := h.run("", args...)
	if code != ExitOK {
		h.t.Fatalf("%v: exit %d: %s", args, code, stderr)
	}
	return stdout
}

func (h *harness) fails(exit int, code string, args ...string) domain.Error {
	h.t.Helper()
	got, stdout, stderr := h.run("", args...)
	if got != exit {
		h.t.Fatalf("%v: exit %d, want %d; stdout %s stderr %s", args, got, exit, stdout, stderr)
	}
	var envelope struct {
		Error domain.Error `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &envelope); err != nil {
		h.t.Fatalf("%v: stderr is not a JSON error: %q", args, stderr)
	}
	if envelope.Error.Code != code {
		h.t.Fatalf("%v: error %+v, want %s", args, envelope.Error, code)
	}
	return envelope.Error
}

func decodeJSON[T any](t *testing.T, text string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	return value
}

func TestInitAndCheck(t *testing.T) {
	h := newHarness(t)
	status := decodeJSON[workspace.Status](t, h.ok("check"))
	if !status.DatabaseExists || status.ProjectID == "" {
		t.Fatalf("status %+v", status)
	}
	if _, err := os.Stat(filepath.Join(h.repo, projectconfig.IdentityFileName)); err != nil {
		t.Fatal(err)
	}
	h.fails(ExitError, projectconfig.CodeProjectAlreadyInitialized, "init")
	h.fails(ExitError, projectconfig.CodeProjectNotFound, "check", "--dir", t.TempDir())
}

func TestTaskLifecycleCommands(t *testing.T) {
	h := newHarness(t)
	created := decodeJSON[ops.TaskResult](t, h.ok("task", "create", "--title", "First", "--acceptance", "it works")).Task
	if created.Number != 1 || created.State != nil || created.AcceptanceCriteria != "it works" {
		t.Fatalf("created %+v", created)
	}
	h.ok("task", "create", "--title", "Second")

	// Flags may follow positional arguments.
	updated := decodeJSON[ops.TaskResult](t, h.ok("task", "update", "1", "--version", "1", "--description", "")).Task
	if updated.Version != 1 {
		t.Fatalf("no-op update changed version: %+v", updated)
	}
	updated = decodeJSON[ops.TaskResult](t, h.ok("task", "update", "#1", "--title", "First!", "--version", "1")).Task
	if updated.Title != "First!" || updated.Version != 2 {
		t.Fatalf("updated %+v", updated)
	}
	h.ok("task", "queue", "1", "--version", "2")
	h.ok("task", "queue", "2", "--version", "1")

	ready := decodeJSON[domain.ReadyQueue](t, h.ok("ready", "list"))
	if len(ready.Tasks) != 2 || ready.Tasks[0].Number != 1 {
		t.Fatalf("ready %+v", ready)
	}
	reordered := decodeJSON[domain.ReadyQueue](t, h.ok("ready", "reorder", "--version", strconv.FormatInt(ready.Version, 10), "2", "1"))
	if reordered.Tasks[0].Number != 2 {
		t.Fatalf("reordered %+v", reordered)
	}

	blocked := decodeJSON[ops.TaskResult](t, h.ok("task", "set-state", "1", "BLOCKED", "--version", "3", "--reason", "needs a decision")).Task
	if *blocked.State != domain.StateBlocked || *blocked.StateReason != "needs a decision" {
		t.Fatalf("blocked %+v", blocked)
	}
	listed := decodeJSON[ops.TasksResult](t, h.ok("task", "list", "--state", "BLOCKED,DONE"))
	if len(listed.Tasks) != 1 || listed.Tasks[0].Number != 1 {
		t.Fatalf("listed %+v", listed)
	}
	if n := len(decodeJSON[ops.TasksResult](t, h.ok("task", "list", "--unqueued")).Tasks); n != 0 {
		t.Fatalf("%d unqueued", n)
	}

	h.ok("fact", "record", "1", "--kind", "handoff", "--body", "context", "--data", `{"branch":"ab-2"}`)
	code, _, stderr := h.run("delivery evidence\n", "fact", "record", "1", "--kind", "delivery", "--body-file", "-", "--actor", "developer")
	if code != ExitOK {
		t.Fatal(stderr)
	}
	facts := decodeJSON[ops.FactsResult](t, h.ok("fact", "list", "1", "--kind", "delivery")).Facts
	if len(facts) != 1 || facts[0].Body != "delivery evidence\n" || facts[0].Actor != "developer" {
		t.Fatalf("facts %+v", facts)
	}

	history := decodeJSON[struct {
		Task   domain.Task        `json:"task"`
		Facts  []domain.TaskFact  `json:"facts"`
		Events []domain.TaskEvent `json:"events"`
	}](t, h.ok("history", "1"))
	if len(history.Facts) != 2 || len(history.Events) != 6 || history.Task.Number != 1 {
		t.Fatalf("history %d facts %d events", len(history.Facts), len(history.Events))
	}
	page := decodeJSON[ops.EventsResult](t, h.ok("events", "--after", "2", "--limit", "3"))
	if len(page.Events) != 3 || page.Events[0].ID != 3 {
		t.Fatalf("page %+v", page)
	}

	view := decodeJSON[struct {
		ProjectID string            `json:"project_id"`
		Ready     domain.ReadyQueue `json:"ready"`
		Tasks     []domain.Task     `json:"tasks"`
	}](t, h.ok("board"))
	if len(view.Tasks) != 2 || len(view.Ready.Tasks) != 1 || view.ProjectID == "" {
		t.Fatalf("board %+v", view)
	}
}

// call dispatches through the same ops.Service.Call as MCP tools/call.
func TestCallMatchesTypedCommands(t *testing.T) {
	h := newHarness(t)
	viaCall := decodeJSON[ops.TaskResult](t, h.ok("call", "create_task", `{"title":"raw","idempotency_key":"k"}`)).Task
	code, stdout, stderr := h.run(`{"title":"raw","idempotency_key":"k"}`, "call", "create_task", "-")
	if code != ExitOK {
		t.Fatal(stderr)
	}
	if replay := decodeJSON[ops.TaskResult](t, stdout).Task; replay.ID != viaCall.ID {
		t.Fatal("idempotent replay created a new task")
	}
	viaCommand := decodeJSON[ops.TaskResult](t, h.ok("task", "get", "1")).Task
	if viaCommand != viaCall {
		t.Fatalf("call %+v, command %+v", viaCall, viaCommand)
	}
	h.fails(ExitError, domain.CodeIdempotencyConflict, "task", "create", "--title", "other", "--key", "k")
	h.fails(ExitError, domain.CodeInvalidArgument, "call", "create_task", `{"title":"x","status":"READY"}`)
	h.fails(ExitError, domain.CodeInvalidArgument, "call", "claim_task", `{}`)

	var listed []map[string]any
	if err := json.Unmarshal([]byte(h.ok("operations")), &listed); err != nil || len(listed) != len(ops.Operations()) {
		t.Fatalf("operations: %v %d", err, len(listed))
	}
}

func TestErrorsKeepTheirCodes(t *testing.T) {
	h := newHarness(t)
	h.ok("task", "create", "--title", "a")
	conflict := h.fails(ExitError, domain.CodeVersionConflict, "task", "queue", "1", "--version", "9")
	if !conflict.Retryable {
		t.Fatalf("conflict %+v", conflict)
	}
	h.fails(ExitError, domain.CodeTaskNotQueued, "task", "set-state", "1", "DONE", "--version", "1")
	h.fails(ExitError, domain.CodeInvalidArgument, "task", "set-state", "1", "REVIEW", "--version", "1")
	h.fails(ExitError, domain.CodeTaskNotFound, "task", "get", "42")
	h.fails(ExitError, domain.CodeInvalidArgument, "fact", "record", "1", "--kind", "note", "--body", "x", "--data", "[1]")
	h.fails(ExitError, domain.CodeInvalidArgument, "task", "create", "--title", "x", "--actor", " ")

	h.fails(ExitUsage, CodeUsage, "task", "queue", "1")
	h.fails(ExitUsage, CodeUsage, "task", "get")
	h.fails(ExitUsage, CodeUsage, "task", "launch")
	h.fails(ExitUsage, CodeUsage, "task", "list", "--queued", "--unqueued")
	h.fails(ExitUsage, CodeUsage, "task", "create", "--bogus")
	h.fails(ExitUsage, CodeUsage)

	if code, stdout, _ := h.run("", "help"); code != ExitOK || !strings.Contains(stdout, "agent-board") {
		t.Fatalf("help exit %d", code)
	}
}

// mcp serves the discovered project's Board over stdio and exits cleanly
// when the client closes stdin.
func TestMCPCommand(t *testing.T) {
	h := newHarness(t)
	h.ok("task", "create", "--title", "seen over MCP")

	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	var stderr bytes.Buffer
	env := h.env
	env.Stdin, env.Stdout, env.Stderr = serverIn, serverOut, &stderr
	exit := make(chan int, 1)
	go func() {
		exit <- Run(context.Background(), []string{"mcp", "--actor", "stdio"}, env)
		serverOut.Close()
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_task", Arguments: map[string]any{"task": "1"}})
	if err != nil || result.IsError {
		t.Fatalf("get_task: %v %+v", err, result)
	}
	if !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "seen over MCP") {
		t.Fatalf("result %+v", result.Content[0])
	}
	session.Close()
	if code := <-exit; code != ExitOK {
		t.Fatalf("mcp exit %d: %s", code, stderr.String())
	}
}

// Adapters reach storage only through the Board service: they never import
// the SQLite layer, migrations, or database/sql.
func TestAdaptersDoNotTouchStorage(t *testing.T) {
	forbidden := []string{
		"database/sql",
		"modernc.org/sqlite",
		"github.com/boboty/agent-board/internal/sqlite",
		"github.com/boboty/agent-board/migrations",
	}
	for _, dir := range []string{".", "../mcpserver", "../ops", "../../cmd/agent-board"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range parsed.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				for _, bad := range forbidden {
					if path == bad || strings.HasPrefix(path, bad+"/") {
						t.Errorf("%s imports %s", file, path)
					}
				}
			}
		}
	}
}
