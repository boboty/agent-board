package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ops"
	"github.com/boboty/agent-board/internal/projectconfig"
	"github.com/boboty/agent-board/internal/workspace"
	"github.com/boboty/agent-board/workflow"
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
		Home:    home,
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

func TestSkillCommands(t *testing.T) {
	home := t.TempDir()
	h := &harness{t: t, env: Env{Home: home}}
	targets := []struct {
		harness string
		path    string
	}{
		{"claude-code", filepath.Join(home, ".claude", "skills", "agent-board-workflow", "SKILL.md")},
		{"codex", filepath.Join(home, ".codex", "skills", "agent-board-workflow", "SKILL.md")},
		{"opencode", filepath.Join(home, ".agents", "skills", "agent-board-workflow", "SKILL.md")},
	}

	check := func(want string) {
		t.Helper()
		result := decodeJSON[struct {
			Skill  string `json:"skill"`
			Status []struct {
				Harness string `json:"harness"`
				Path    string `json:"path"`
				Status  string `json:"status"`
			} `json:"status"`
		}](t, h.ok("skill", "check"))
		if result.Skill != "agent-board-workflow" || len(result.Status) != len(targets) {
			t.Fatalf("skill check: %+v", result)
		}
		for i, got := range result.Status {
			if got.Harness != targets[i].harness || got.Path != targets[i].path || got.Status != want {
				t.Fatalf("skill status[%d] = %+v, want %s at %s", i, got, want, targets[i].path)
			}
		}
	}

	check("missing")
	show := h.ok("skill", "show")
	if show != workflow.Skill {
		t.Fatal("skill show differs from embedded content")
	}
	h.ok("skill", "install")
	check("current")
	for _, target := range targets {
		contents, err := os.ReadFile(target.path)
		if err != nil || string(contents) != show {
			t.Fatalf("installed %s skill mismatch: %v", target.harness, err)
		}
	}
	if err := os.WriteFile(targets[1].path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := decodeJSON[struct {
		Status []struct {
			Status string `json:"status"`
		} `json:"status"`
	}](t, h.ok("skill", "check"))
	if result.Status[1].Status != "different" {
		t.Fatalf("different content status = %+v", result.Status)
	}
	h.ok("skill", "install")
	check("current")
	h.fails(ExitUsage, CodeUsage, "skill", "show", "extra")
}

func TestDoctorReportsHealthyAndMissingComponentsWithoutWriting(t *testing.T) {
	h := newHarness(t)
	h.ok("skill", "install")
	output := h.ok("doctor")
	for _, want := range []string{"Binary   OK", "Skill    OK", "Project  OK", "Board    PRESENT", "SQLite 文件头有效；未验证 schema 或 project binding", "MCP      OK", "operations and schemas loaded"} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output %q does not contain %q", output, want)
		}
	}

	database := filepath.Join(h.env.Home, ".agent-board", decodeJSON[workspace.Status](t, h.ok("check")).ProjectID, "board.db")
	before, err := os.Stat(database)
	if err != nil {
		t.Fatal(err)
	}
	if code, out, stderr := h.run("", "doctor"); code != ExitOK || stderr != "" || out != output {
		t.Fatalf("repeat doctor exit %d stderr %q output differs", code, stderr)
	}
	after, err := os.Stat(database)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatalf("doctor changed database metadata: before %v/%d after %v/%d", before.ModTime(), before.Size(), after.ModTime(), after.Size())
	}
	codexSkill := filepath.Join(h.env.Home, ".codex", "skills", "agent-board-workflow", "SKILL.md")
	if err := os.WriteFile(codexSkill, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := h.run("", "doctor")
	if code != ExitError || stderr != "" || !strings.Contains(out, "Skill    PROBLEM") || !strings.Contains(out, "codex=different") || !strings.Contains(out, "Next step:") {
		t.Fatalf("different Skill doctor exit %d stdout %q stderr %q", code, out, stderr)
	}
	if err := os.WriteFile(codexSkill, []byte(workflow.Skill), 0o644); err != nil {
		t.Fatal(err)
	}
	garbage := []byte("GARBAGE NOT A SQLITE FILE")
	if err := os.WriteFile(database, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr = h.run("", "doctor")
	if code != ExitError || stderr != "" || !strings.Contains(out, "Board    PROBLEM") || !strings.Contains(out, "不是有效的 SQLite 文件头") || !strings.Contains(out, "Next step:") {
		t.Fatalf("garbage database doctor exit %d stdout %q stderr %q", code, out, stderr)
	}
	afterGarbage, err := os.ReadFile(database)
	if err != nil || !bytes.Equal(afterGarbage, garbage) {
		t.Fatalf("doctor changed garbage database: %v", err)
	}

	if err := os.Remove(database); err != nil {
		t.Fatal(err)
	}
	code, out, stderr = h.run("", "doctor")
	if code != ExitError || stderr != "" || !strings.Contains(out, "Board    MISSING") || !strings.Contains(out, "Next step:") {
		t.Fatalf("missing database doctor exit %d stdout %q stderr %q", code, out, stderr)
	}
	if _, err := os.Stat(database); !os.IsNotExist(err) {
		t.Fatalf("doctor created missing database: %v", err)
	}
}

func TestDoctorOutsideProjectAndWithMissingSkill(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	h := &harness{t: t, repo: dir, env: Env{Dir: dir, Home: home, Version: "test"}}
	code, output, stderr := h.run("", "doctor")
	if code != ExitError || stderr != "" {
		t.Fatalf("doctor exit %d stdout %q stderr %q", code, output, stderr)
	}
	for _, want := range []string{"Binary   OK", "Skill    MISSING", "Project  MISSING", "Board    MISSING", "MCP      OK", "agent-board init", "agent-board skill install"} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output %q does not contain %q", output, want)
		}
	}
	for _, path := range []string{filepath.Join(home, ".agent-board"), filepath.Join(home, ".claude"), filepath.Join(home, ".codex"), filepath.Join(home, ".agents")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("doctor created %s: %v", path, err)
		}
	}
	h.fails(ExitUsage, CodeUsage, "doctor", "unexpected")
}

func TestMCPConfigCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent board binary")
	h := &harness{t: t, env: Env{
		ExecutablePath: func() (string, error) { return path, nil },
	}}
	checks := []struct {
		harness  string
		contains string
	}{
		{"generic", `"args": [`},
		{"claude-code", `claude mcp add --transport stdio --scope user agent-board -- '`},
		{"codex", `[mcp_servers.agent-board]`},
		{"opencode", `"type": "local"`},
	}
	for _, check := range checks {
		got := h.ok("mcp", "config", check.harness)
		if !strings.Contains(got, check.contains) || !strings.Contains(got, path) {
			t.Errorf("%s config = %q; want marker %q and executable %q", check.harness, got, check.contains, path)
		}
	}
	generic := h.ok("mcp", "config")
	if !strings.Contains(generic, `"command": "`+path+`"`) {
		t.Fatalf("default generic config lacks injected executable: %q", generic)
	}
	h.fails(ExitUsage, CodeUsage, "mcp", "config", "unknown")
	h.fails(ExitUsage, CodeUsage, "mcp", "config", "codex", "extra")
	h.env.ExecutablePath = func() (string, error) { return "relative/agent-board", nil }
	h.fails(ExitError, "INTERNAL", "mcp", "config")
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

func TestWebCommand(t *testing.T) {
	h := newHarness(t)
	h.fails(ExitUsage, CodeUsage, "web", "--addr", "0.0.0.0:7420")
	h.fails(ExitUsage, CodeUsage, "web", "--addr", "localhost:7420")

	h.env.Actor = ""
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	env := h.env
	env.Stdin, env.Stdout, env.Stderr = strings.NewReader(""), writer, io.Discard
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"web", "--addr", "127.0.0.1:0"}, env) }()
	var started struct {
		URL       string `json:"url"`
		ProjectID string `json:"project_id"`
		Actor     string `json:"actor"`
	}
	if err := json.NewDecoder(reader).Decode(&started); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(started.URL, "http://127.0.0.1:") || started.Actor != DefaultWebActor || started.ProjectID == "" {
		t.Fatalf("started %+v", started)
	}
	response, err := http.Get(started.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), started.ProjectID) {
		t.Fatalf("status %d", response.StatusCode)
	}
	cancel()
	if code := <-done; code != ExitOK {
		t.Fatalf("web exited %d", code)
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
	for _, dir := range []string{".", "../mcpserver", "../ops", "../web", "../../cmd/agent-board"} {
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
