package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ops"
	"github.com/boboty/agent-board/internal/workspace"
	"github.com/boboty/agent-board/management"
	"github.com/boboty/agent-board/workflow"
)

var binary string

// TestMain builds the real binary so these tests exercise separate OS
// processes sharing one database, as harnesses in different worktrees do.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "aboard-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "aboard")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type project struct {
	t    *testing.T
	home string
	repo string
}

func newProject(t *testing.T) *project {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := &project{t: t, home: filepath.Join(base, "home"), repo: filepath.Join(base, "repo")}
	actualHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(p.home) == filepath.Clean(actualHome) || filepath.Clean(p.repo) == filepath.Clean(actualHome) {
		t.Fatal("clean test must use an isolated HOME and project")
	}
	for _, dir := range []string{p.home, p.repo} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p.mustRun(p.repo, "init")
	return p
}

func TestCleanConfirmedRemovesOnlyCurrentProject(t *testing.T) {
	p := newProject(t)
	otherRepo := filepath.Join(filepath.Dir(p.repo), "other-project")
	if err := os.Mkdir(otherRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	p.mustRun(otherRepo, "init")
	identityPath := filepath.Join(p.repo, ".agent-board.json")
	var identity struct {
		ProjectID string `json:"project_id"`
	}
	identity = decode[struct {
		ProjectID string `json:"project_id"`
	}](t, mustRead(t, identityPath))
	dataDir := filepath.Join(p.home, ".agent-board", identity.ProjectID)
	otherID := decode[struct {
		ProjectID string `json:"project_id"`
	}](t, mustRead(t, filepath.Join(otherRepo, ".agent-board.json"))).ProjectID
	otherData := filepath.Join(p.home, ".agent-board", otherID)
	otherDBPath := filepath.Join(otherData, "board.db")
	otherDBBefore, err := os.ReadFile(otherDBPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p.home, ".agents", "skills", "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.home, ".agents", "skills", "keep", "SKILL.md"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.repo, "project-file.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := p.run(p.repo, "clean", "--yes")
	t.Logf("clean --yes: exit=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	if r.code != 0 || !strings.Contains(r.stdout, identityPath) || !strings.Contains(r.stdout, dataDir) {
		t.Fatalf("clean output: %+v", r)
	}
	for _, removed := range []string{identityPath, dataDir} {
		if _, err := os.Lstat(removed); !os.IsNotExist(err) {
			t.Fatalf("expected removed path %s, stat err %v", removed, err)
		}
	}
	for _, kept := range []string{otherData, filepath.Join(otherRepo, ".agent-board.json"), filepath.Join(p.home, ".agents", "skills", "keep", "SKILL.md"), filepath.Join(p.repo, "project-file.txt"), binary} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("expected retained path %s: %v", kept, err)
		}
	}
	otherDBAfter, err := os.ReadFile(otherDBPath)
	if err != nil || !bytes.Equal(otherDBAfter, otherDBBefore) {
		t.Fatalf("other project Board data changed: read err %v", err)
	}
	if got := mustRead(t, filepath.Join(p.home, ".agents", "skills", "keep", "SKILL.md")); got != "keep" {
		t.Fatalf("Skill contents changed: %q", got)
	}
	if got := mustRead(t, filepath.Join(p.repo, "project-file.txt")); got != "keep" {
		t.Fatalf("project file contents changed: %q", got)
	}
}

func TestCleanDeclinesByDefaultAndOnNo(t *testing.T) {
	for _, input := range []string{"", "n\n"} {
		p := newProject(t)
		r := p.runWithInput(p.repo, input, "clean")
		t.Logf("clean input=%q: exit=%d stdout=%q stderr=%q", input, r.code, r.stdout, r.stderr)
		if r.code != 0 || !strings.Contains(r.stdout, "nothing was removed") {
			t.Fatalf("clean refusal output: %+v", r)
		}
		for _, path := range []string{filepath.Join(p.repo, ".agent-board.json"), filepath.Join(p.home, ".agent-board")} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("refusal changed %s: %v", path, err)
			}
		}
	}
}

func TestUninstallIsConfinedAndReportsActualResults(t *testing.T) {
	actualHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	actualExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	actualExecutable, err = filepath.EvalSymlinks(actualExecutable)
	if err != nil {
		t.Fatal(err)
	}
	// Each case runs an isolated copy of the real CLI in an uninitialized cwd.
	// No invocation points at the developer's installed or test-build binary.
	setup := func() (string, string, string, string) {
		t.Helper()
		base, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		home := filepath.Join(base, "home")
		cwd := filepath.Join(base, "uninitialized")
		fixture := filepath.Join(base, "bin", "aboard")
		if filepath.Clean(home) == filepath.Clean(actualHome) || filepath.Clean(fixture) == filepath.Clean(actualExecutable) || filepath.Clean(fixture) == filepath.Clean(binary) || filepath.Clean(fixture) == filepath.Join(filepath.Clean(actualHome), "go", "bin", "aboard") || filepath.Clean(cwd) == filepath.Clean(actualHome) {
			t.Fatal("uninstall fixture could target a real user path")
		}
		for _, dir := range []string{home, cwd, filepath.Dir(fixture)} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		contents, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixture, contents, 0o755); err != nil {
			t.Fatal(err)
		}
		return base, home, cwd, fixture
	}
	install := func(home string, skillName, content string) string {
		t.Helper()
		path := filepath.Join(home, ".agents", "skills", "agent-board-"+skillName, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	run := func(home, cwd, fixture, input string, args ...string) (int, string, string) {
		t.Helper()
		cmd := exec.Command(fixture, append([]string{"uninstall"}, args...)...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = strings.NewReader(input)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else {
				t.Fatalf("run isolated uninstall: %v", err)
			}
		}
		return code, stdout.String(), stderr.String()
	}
	assertAbsent := func(path string) {
		t.Helper()
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("expected absent %s, lstat err=%v", path, err)
		}
	}

	// Normal uninstall: matching Skills and all data are removed, the isolated
	// executable unlinks itself on the actual host platform, and unrelated files remain.
	_, home, cwd, fixture := setup()
	managed := install(home, "workflow", workflow.Skill)
	managementPath := install(home, "management", management.Skill)
	data := filepath.Join(home, ".agent-board")
	if err := os.MkdirAll(filepath.Join(data, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "nested", "db"), []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "keep.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, output, stderr := run(home, cwd, fixture, "", "--yes")
	t.Logf("uninstall --yes output (darwin=%t):\n%sstderr=%q exit=%d", runtime.GOOS == "darwin", output, stderr, code)
	if code != 0 || stderr != "" || !strings.Contains(output, "Agent Board uninstall complete.") {
		t.Fatalf("normal uninstall: exit=%d stdout=%q stderr=%q", code, output, stderr)
	}
	for _, path := range []string{managed, managementPath, data, fixture} {
		assertAbsent(path)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "keep" {
		t.Fatalf("outside file changed: %q, %v", got, err)
	}
	if _, err := os.Stat(cwd); err != nil {
		t.Fatalf("uninitialized cwd was removed: %v", err)
	}

	// A modified managed Skill is retained by default and the output identifies
	// it exactly; --force removes it only after a separate affirmative answer.
	_, home, cwd, fixture = setup()
	modified := install(home, "workflow", "locally modified Agent Board workflow\n")
	data = filepath.Join(home, ".agent-board")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	code, output, stderr = run(home, cwd, fixture, "y\n")
	t.Logf("modified Skill default output:\n%sstderr=%q exit=%d", output, stderr, code)
	if code == 0 || !strings.Contains(output, "PRESERVED modified Agent Board Skill") || !strings.Contains(output, "left in place; remove manually") || !strings.Contains(output, "reinstall aboard before using --force") || strings.Contains(output, "use --force to remove after confirming uninstall") || !strings.Contains(output, "Preserved items requiring manual handling:\n  "+modified) || !strings.Contains(output, "Binary: REMOVED ("+fixture+")") || !strings.Contains(output, "Agent Board uninstall incomplete") {
		t.Fatalf("modified default report: exit=%d stdout=%q stderr=%q", code, output, stderr)
	}
	if got, err := os.ReadFile(modified); err != nil || string(got) != "locally modified Agent Board workflow\n" {
		t.Fatalf("modified Skill was changed: %q, %v", got, err)
	}
	assertAbsent(data)
	assertAbsent(fixture)
	_, home, cwd, fixture = setup()
	modified = install(home, "workflow", "locally modified Agent Board workflow\n")
	code, output, stderr = run(home, cwd, fixture, "yes\n", "--force")
	t.Logf("uninstall --force with confirmation output:\n%sstderr=%q exit=%d", output, stderr, code)
	if code != 0 || stderr != "" || !strings.Contains(output, "REMOVED Skill: "+modified) || !strings.Contains(output, "Agent Board uninstall complete.") {
		t.Fatalf("forced uninstall: exit=%d stdout=%q stderr=%q", code, output, stderr)
	}
	assertAbsent(modified)
	assertAbsent(fixture)

	// A changed legacy Codex copy is unmanaged by the existing install/check
	// behavior, so --force must preserve it and list it for manual handling.
	_, home, cwd, fixture = setup()
	legacy := filepath.Join(home, ".codex", "skills", "agent-board-workflow", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("unmanaged legacy workflow\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, output, stderr = run(home, cwd, fixture, "yes\n", "--force")
	t.Logf("codex-legacy different Skill with --force output:\n%sstderr=%q exit=%d", output, stderr, code)
	if code == 0 || stderr != "" || !strings.Contains(output, "PRESERVED unmanaged Skill (left in place") || !strings.Contains(output, legacy) || strings.Contains(output, "PRESERVED modified Agent Board Skill") || !strings.Contains(output, "Preserved items requiring manual handling:\n  "+legacy) || !strings.Contains(output, "Binary: REMOVED ("+fixture+")") {
		t.Fatalf("legacy --force report: exit=%d stdout=%q stderr=%q", code, output, stderr)
	}
	if got, err := os.ReadFile(legacy); err != nil || string(got) != "unmanaged legacy workflow\n" {
		t.Fatalf("unmanaged legacy Skill changed: %q, %v", got, err)
	}
	assertAbsent(fixture)

	// Symlinked known targets and a symlinked data root are preserved so a
	// known HOME path cannot redirect deletion outside the Agent Board paths.
	base, home, cwd, fixture := setup()
	externalSkill := filepath.Join(base, "external-skill.md")
	if err := os.WriteFile(externalSkill, []byte("outside skill data"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedSkill := filepath.Join(home, ".agents", "skills", "agent-board-workflow", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(linkedSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalSkill, linkedSkill); err != nil {
		t.Fatal(err)
	}
	externalData := filepath.Join(base, "external-data")
	if err := os.Mkdir(externalData, 0o700); err != nil {
		t.Fatal(err)
	}
	dataMarker := filepath.Join(externalData, "keep.db")
	if err := os.WriteFile(dataMarker, []byte("outside board data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalData, filepath.Join(home, ".agent-board")); err != nil {
		t.Fatal(err)
	}
	code, output, stderr = run(home, cwd, fixture, "y\n")
	t.Logf("symlink boundary output:\n%sstderr=%q exit=%d", output, stderr, code)
	if code == 0 || !strings.Contains(output, "PRESERVED unmanaged Skill (left in place") || !strings.Contains(output, linkedSkill) || !strings.Contains(output, "FAILED local data") || !strings.Contains(output, "Local data remains or may be partially removed; inspect and remove it manually") || !strings.Contains(output, "Failed items requiring inspection and manual cleanup:\n  "+filepath.Join(home, ".agent-board")) || !strings.Contains(output, "Agent Board uninstall incomplete") {
		t.Fatalf("symlink boundary: exit=%d stdout=%q stderr=%q", code, output, stderr)
	}
	for _, path := range []string{linkedSkill, filepath.Join(home, ".agent-board")} {
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("expected preserved symlink %s: info=%v err=%v", path, info, err)
		}
	}
	if got, err := os.ReadFile(externalSkill); err != nil || string(got) != "outside skill data" {
		t.Fatalf("external Skill target changed: %q, %v", got, err)
	}
	if got, err := os.ReadFile(dataMarker); err != nil || string(got) != "outside board data" {
		t.Fatalf("external data target changed: %q, %v", got, err)
	}
	assertAbsent(fixture)

	// --force is not confirmation. Default EOF and an explicit No both leave
	// every listed target untouched and never claim completion.
	for _, refusal := range []struct{ name, input string }{{"default", ""}, {"no", "n\n"}} {
		_, home, cwd, fixture = setup()
		managed = install(home, "workflow", workflow.Skill)
		data = filepath.Join(home, ".agent-board")
		if err := os.Mkdir(data, 0o700); err != nil {
			t.Fatal(err)
		}
		code, output, stderr = run(home, cwd, fixture, refusal.input, "--force")
		t.Logf("uninstall --force %s refusal output:\n%sstderr=%q exit=%d", refusal.name, output, stderr, code)
		if code != 0 || stderr != "" || !strings.Contains(output, "Uninstall cancelled; nothing was removed.") || strings.Contains(output, "uninstall complete") {
			t.Fatalf("%s refusal: exit=%d stdout=%q stderr=%q", refusal.name, code, output, stderr)
		}
		for _, path := range []string{managed, data, fixture} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("refusal removed %s: %v", path, err)
			}
		}
	}
	if filepath.Clean(home) == filepath.Clean(actualHome) {
		t.Fatal("final fixture HOME equals real HOME")
	}
}

func TestCleanInteractiveConfirmation(t *testing.T) {
	p := newProject(t)
	identityPath := filepath.Join(p.repo, ".agent-board.json")
	identity := decode[struct {
		ProjectID string `json:"project_id"`
	}](t, mustRead(t, identityPath))
	dataDir := filepath.Join(p.home, ".agent-board", identity.ProjectID)
	r := p.runWithInput(p.repo, "yes\n", "clean")
	t.Logf("clean interactive yes: exit=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	if r.code != 0 || !strings.Contains(r.stdout, "Project Agent Board data and identity removed.") {
		t.Fatalf("interactive confirmation output: %+v", r)
	}
	for _, path := range []string{identityPath, dataDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected removed path %s, stat err %v", path, err)
		}
	}
}

func TestCleanIdentityRemovalFailureReportsPartialCleanup(t *testing.T) {
	p := newProject(t)
	identityPath := filepath.Join(p.repo, ".agent-board.json")
	identity := decode[struct {
		ProjectID string `json:"project_id"`
	}](t, mustRead(t, identityPath))
	dataDir := filepath.Join(p.home, ".agent-board", identity.ProjectID)
	if err := os.Chmod(p.repo, 0o555); err != nil {
		t.Fatal(err)
	}
	r := p.run(p.repo, "clean", "--yes")
	if err := os.Chmod(p.repo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Logf("clean identity removal failure: exit=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	if r.code != 1 || !strings.Contains(r.stderr, "Board data was removed, but cannot remove project identity") {
		t.Fatalf("identity failure output: %+v", r)
	}
	if _, err := os.Stat(identityPath); err != nil {
		t.Fatalf("identity missing after failed removal: %v", err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("data remains after successful data removal, stat err %v", err)
	}
}

func TestCleanMissingInvalidIdentityAndDataFailureKeepIdentity(t *testing.T) {
	p := newProject(t)
	identityPath := filepath.Join(p.repo, ".agent-board.json")
	projectID := decode[struct {
		ProjectID string `json:"project_id"`
	}](t, mustRead(t, identityPath)).ProjectID
	dataDir := filepath.Join(p.home, ".agent-board", projectID)
	if err := os.WriteFile(identityPath, []byte(`{"version":2,"project_id":"bad","name":"fixture"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := p.run(p.repo, "clean", "--yes")
	t.Logf("clean invalid identity: exit=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	if r.code != 1 || r.errorCode(t) != "INVALID_PROJECT_IDENTITY" {
		t.Fatalf("invalid identity output: %+v", r)
	}
	if _, err := os.Stat(identityPath); err != nil {
		t.Fatalf("invalid identity was removed: %v", err)
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Fatalf("data changed for invalid identity: %v", err)
	}
	missingRepo := filepath.Join(filepath.Dir(p.repo), "missing")
	if err := os.Mkdir(missingRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	r = p.run(missingRepo, "clean", "--yes")
	t.Logf("clean missing identity: exit=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	if r.code != 1 || r.errorCode(t) != "PROJECT_NOT_FOUND" {
		t.Fatalf("missing identity output: %+v", r)
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Fatalf("data changed for missing identity: %v", err)
	}

	// Restore a valid identity, then make the data-root path a regular file.
	if err := os.WriteFile(identityPath, []byte(`{"version":2,"project_id":"01M3VN4DT676SGJ90T58JRB13R","name":"fixture"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(p.home, ".agent-board")
	if err := os.RemoveAll(dataRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataRoot, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	r = p.run(p.repo, "clean", "--yes")
	t.Logf("clean data removal failure: exit=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	if r.code != 1 || !strings.Contains(r.stderr, "project identity was kept") {
		t.Fatalf("data failure output: %+v", r)
	}
	if _, err := os.Stat(identityPath); err != nil {
		t.Fatalf("identity removed despite data failure: %v", err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func (p *project) environ() []string {
	return append(os.Environ(), "HOME="+p.home, "USERPROFILE="+p.home, "AGENT_BOARD_ACTOR=e2e")
}

func (p *project) command(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = p.environ()
	return cmd
}

type result struct {
	code           int
	stdout, stderr string
}

func (p *project) run(dir string, args ...string) result {
	return p.runWithInput(dir, "", args...)
}

func (p *project) runWithInput(dir, input string, args ...string) result {
	cmd := p.command(dir, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Stdin = strings.NewReader(input)
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		p.t.Fatal(err)
	}
	return result{code, stdout.String(), stderr.String()}
}

func (p *project) mustRun(dir string, args ...string) string {
	p.t.Helper()
	r := p.run(dir, args...)
	if r.code != 0 {
		p.t.Fatalf("%v: exit %d: %s", args, r.code, r.stderr)
	}
	return r.stdout
}

func (r result) errorCode(t *testing.T) string {
	t.Helper()
	var envelope struct {
		Error domain.Error `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stderr), &envelope); err != nil {
		t.Fatalf("stderr is not a JSON error: %q", r.stderr)
	}
	return envelope.Error.Code
}

func decode[T any](t *testing.T, text string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	return value
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (p *project) connectMCP(dir string) *mcp.ClientSession {
	p.t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.CommandTransport{Command: p.command(dir, "mcp", "--actor", "mcp-harness")}, nil)
	if err != nil {
		p.t.Fatal(err)
	}
	p.t.Cleanup(func() { session.Close() })
	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func structured[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	return decode[T](t, string(encoded))
}

// A CLI process in one git worktree and an MCP server process in another
// resolve the same database and see each other's writes.
func TestWorktreesAndAdaptersShareOneBoard(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	p := newProject(t)
	git(t, p.repo, "init", "-q")
	git(t, p.repo, "add", ".agent-board.json")
	git(t, p.repo, "commit", "-q", "-m", "identity")
	worktree := filepath.Join(filepath.Dir(p.repo), "worktree")
	git(t, p.repo, "worktree", "add", "-q", worktree)
	nested := filepath.Join(worktree, "sub", "dir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	mainStatus := decode[workspace.Status](t, p.mustRun(p.repo, "check"))
	worktreeStatus := decode[workspace.Status](t, p.mustRun(nested, "check"))
	if worktreeStatus.ProjectRoot != worktree || worktreeStatus.DatabasePath != mainStatus.DatabasePath {
		t.Fatalf("main %+v, worktree %+v", mainStatus, worktreeStatus)
	}

	created := decode[ops.TaskResult](t, p.mustRun(nested, "task", "create", "--title", "from worktree CLI")).Task

	session := p.connectMCP(p.repo)
	got := structured[ops.TaskResult](t, callTool(t, session, "get_task", map[string]any{"task": created.ID})).Task
	if got != created {
		t.Fatalf("MCP saw %+v, CLI created %+v", got, created)
	}
	queued := structured[ops.TaskResult](t, callTool(t, session, "queue_task", map[string]any{"task": "1", "expected_version": 1})).Task
	callTool(t, session, "record_fact", map[string]any{"task": "1", "kind": "execution", "body": "started in main worktree"})

	history := decode[struct {
		Task   domain.Task        `json:"task"`
		Facts  []domain.TaskFact  `json:"facts"`
		Events []domain.TaskEvent `json:"events"`
	}](t, p.mustRun(worktree, "history", "1"))
	if history.Task.Version != queued.Version || len(history.Facts) != 1 || history.Facts[0].Actor != "mcp-harness" {
		t.Fatalf("worktree history %+v", history)
	}
	if actors := []string{history.Events[0].Actor, history.Events[1].Actor}; actors[0] != "e2e" || actors[1] != "mcp-harness" {
		t.Fatalf("event actors %v", actors)
	}
}

// The same stale write fails with the same structured error through both
// adapters.
func TestErrorsMatchAcrossAdapters(t *testing.T) {
	p := newProject(t)
	p.mustRun(p.repo, "task", "create", "--title", "a")
	p.mustRun(p.repo, "task", "queue", "1", "--version", "1")

	cli := p.run(p.repo, "task", "set-state", "1", "DONE", "--version", "1")
	var cliErr struct {
		Error domain.Error `json:"error"`
	}
	if cli.code != 1 || json.Unmarshal([]byte(cli.stderr), &cliErr) != nil {
		t.Fatalf("CLI exit %d stderr %q", cli.code, cli.stderr)
	}
	session := p.connectMCP(p.repo)
	result := callTool(t, session, "set_task_state", map[string]any{"task": "1", "expected_version": 1, "state": "DONE"})
	mcpErr := structured[struct {
		Error domain.Error `json:"error"`
	}](t, result)
	if !result.IsError || mcpErr.Error.Code != domain.CodeVersionConflict {
		t.Fatalf("MCP %+v", mcpErr)
	}
	if fmt.Sprint(cliErr.Error) != fmt.Sprint(mcpErr.Error) {
		t.Fatalf("CLI %+v, MCP %+v", cliErr.Error, mcpErr.Error)
	}
}

// Concurrent writer processes go through the Board's transactions, version
// checks, and idempotency records.
func TestConcurrentProcesses(t *testing.T) {
	p := newProject(t)
	const writers = 8
	parallel := func(args func(i int) []string) []result {
		results := make([]result, writers)
		var wg sync.WaitGroup
		for i := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i] = p.run(p.repo, args(i)...)
			}()
		}
		wg.Wait()
		return results
	}

	// Distinct creates: every process gets its own task number.
	numbers := map[int64]bool{}
	for _, r := range parallel(func(i int) []string {
		return []string{"task", "create", "--title", "t" + strconv.Itoa(i)}
	}) {
		if r.code != 0 {
			t.Fatalf("create: %s", r.stderr)
		}
		numbers[decode[ops.TaskResult](t, r.stdout).Task.Number] = true
	}
	for n := int64(1); n <= writers; n++ {
		if !numbers[n] {
			t.Fatalf("numbers %v", numbers)
		}
	}

	// Competing state changes on one version: exactly one wins.
	p.mustRun(p.repo, "task", "queue", "1", "--version", "1")
	wins := 0
	for _, r := range parallel(func(int) []string {
		return []string{"task", "set-state", "1", "IN_PROGRESS", "--version", "2"}
	}) {
		switch {
		case r.code == 0:
			wins++
		case r.errorCode(t) != domain.CodeVersionConflict:
			t.Fatalf("set-state: %s", r.stderr)
		}
	}
	if wins != 1 {
		t.Fatalf("%d winners", wins)
	}

	// Retries with one idempotency key: one task, one result.
	ids := map[string]bool{}
	for _, r := range parallel(func(int) []string {
		return []string{"task", "create", "--title", "retried", "--key", "retry-1"}
	}) {
		if r.code != 0 {
			t.Fatalf("idempotent create: %s", r.stderr)
		}
		ids[decode[ops.TaskResult](t, r.stdout).Task.ID] = true
	}
	tasks := decode[ops.TasksResult](t, p.mustRun(p.repo, "task", "list")).Tasks
	if len(ids) != 1 || len(tasks) != writers+1 {
		t.Fatalf("%d ids, %d tasks", len(ids), len(tasks))
	}
}
