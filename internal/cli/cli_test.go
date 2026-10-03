package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ops"
	"github.com/boboty/agent-board/internal/projectconfig"
	"github.com/boboty/agent-board/internal/workspace"
	"github.com/boboty/agent-board/management"
	"github.com/boboty/agent-board/workflow"
)

type harness struct {
	t    *testing.T
	repo string
	env  Env
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

// gitRepo returns a fresh, symlink-resolved Git repository.
func gitRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return repo
}

// localIdentity is the identity path inside repo's Git common directory.
func localIdentity(repo string) string {
	return filepath.Join(repo, ".git", projectconfig.IdentityFileName)
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	home := t.TempDir()
	repo := gitRepo(t)
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

func assertDoctorEnglish(t *testing.T, output string) {
	t.Helper()
	for _, r := range output {
		if unicode.Is(unicode.Han, r) || strings.ContainsRune("，。；：（）", r) {
			t.Fatalf("doctor output contains non-English program text %q: %q", r, output)
		}
	}
}

func decodeJSON[T any](t *testing.T, text string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	return value
}

func snapshotTree(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			key := root
			if relative != "." {
				key = filepath.Join(root, relative)
			}
			if entry.IsDir() {
				snapshot[key] = "directory"
				return nil
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			snapshot[key] = fmt.Sprintf("%x", sha256.Sum256(contents))
			return nil
		})
		if err != nil {
			t.Fatalf("snapshot %s: %v", root, err)
		}
	}
	return snapshot
}

func TestInitAndCheck(t *testing.T) {
	h := newHarness(t)
	status := decodeJSON[workspace.Status](t, h.ok("check"))
	if !status.DatabaseExists || status.ProjectID == "" {
		t.Fatalf("status %+v", status)
	}
	identityPath := localIdentity(h.repo)
	if _, err := os.Stat(identityPath); err != nil {
		t.Fatal(err)
	}
	identity := decodeJSON[projectconfig.Identity](t, string(mustRead(t, identityPath)))
	if identity.Version != 2 || identity.Name != filepath.Base(h.repo) {
		t.Fatalf("default identity = %+v, want version 2 and project root basename", identity)
	}
	h.fails(ExitError, projectconfig.CodeProjectAlreadyInitialized, "init")
	h.fails(ExitError, projectconfig.CodeProjectNotFound, "check", "--dir", t.TempDir())
	h.fails(ExitError, projectconfig.CodeProjectNotFound, "check", "--dir", gitRepo(t))
	if _, err := os.Stat(filepath.Join(h.repo, ".agent-board.json")); !os.IsNotExist(err) {
		t.Fatalf("init wrote an identity into the worktree: %v", err)
	}
	custom := gitRepo(t)
	customHarness := &harness{t: t, repo: custom, env: Env{Dir: custom, Home: t.TempDir(), Version: "test"}}
	customHarness.ok("init", "--name", "  Named Project  ")
	customIdentity := decodeJSON[projectconfig.Identity](t, string(mustRead(t, localIdentity(custom))))
	if customIdentity.Name != "Named Project" {
		t.Fatalf("explicit identity name = %q", customIdentity.Name)
	}
	customHarness.fails(ExitUsage, CodeUsage, "init", "--name", " \t ")
}

func TestCleanDisplaysProjectNameBeforeRemoval(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "yes", args: []string{"clean", "--yes"}},
		{name: "interactive confirmation", args: []string{"clean"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			h.ok("board")
			identityPath := localIdentity(h.repo)
			identity := decodeJSON[projectconfig.Identity](t, string(mustRead(t, identityPath)))
			input := ""
			if test.name == "interactive confirmation" {
				input = "yes\n"
			}
			code, output, stderr := h.run(input, test.args...)
			if code != ExitOK || stderr != "" {
				t.Fatalf("clean exit %d stdout %q stderr %q", code, output, stderr)
			}
			nameAt := strings.Index(output, identity.Name)
			if nameAt < 0 {
				t.Fatalf("clean output %q does not contain project name %q", output, identity.Name)
			}
			if len(test.args) == 1 {
				promptAt := strings.Index(output, "Remove this project's Agent Board data and identity?")
				if promptAt < 0 || nameAt > promptAt {
					t.Fatalf("project name is not printed before confirmation prompt: %q", output)
				}
			}
			if _, err := os.Stat(identityPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("project identity still exists after confirmed cleanup: %v", err)
			}
			if _, err := os.Stat(filepath.Join(h.env.Home, ".agent-board", identity.ProjectID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Board data still exists after confirmed cleanup: %v", err)
			}
		})
	}
}

func TestCleanCancellationPreservesProjectContents(t *testing.T) {
	h := newHarness(t)
	h.ok("board")
	identityPath := localIdentity(h.repo)
	identity := decodeJSON[projectconfig.Identity](t, string(mustRead(t, identityPath)))
	dataDir := filepath.Join(h.env.Home, ".agent-board", identity.ProjectID)
	before := snapshotTree(t, h.repo, dataDir)
	code, output, stderr := h.run("no\n", "clean")
	if code != ExitOK || stderr != "" || !strings.Contains(output, identity.Name) || !strings.Contains(output, "Cleanup cancelled; nothing was removed.") {
		t.Fatalf("clean cancellation exit %d stdout %q stderr %q", code, output, stderr)
	}
	if after := snapshotTree(t, h.repo, dataDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("cancelled clean changed project contents: before %v after %v", before, after)
	}
}

func TestSkillCommands(t *testing.T) {
	home := t.TempDir()
	h := &harness{t: t, env: Env{Home: home}}
	targets := func(skill string) []struct{ harness, path string } {
		return []struct{ harness, path string }{
			{"claude-code", filepath.Join(home, ".claude", "skills", "agent-board-"+skill, "SKILL.md")},
			{"codex", filepath.Join(home, ".agents", "skills", "agent-board-"+skill, "SKILL.md")},
			{"opencode", filepath.Join(home, ".claude", "skills", "agent-board-"+skill, "SKILL.md")},
		}
	}
	check := func(skill, want string) {
		t.Helper()
		result := decodeJSON[struct {
			Skills []struct {
				Skill  string `json:"skill"`
				Status []struct {
					Harness string `json:"harness"`
					Path    string `json:"path"`
					Status  string `json:"status"`
				} `json:"status"`
			} `json:"skills"`
		}](t, h.ok("skill", "check", skill))
		if len(result.Skills) != 1 || result.Skills[0].Skill != skill || len(result.Skills[0].Status) != len(targets(skill)) {
			t.Fatalf("skill check: %+v", result)
		}
		for i, got := range result.Skills[0].Status {
			if got.Harness != targets(skill)[i].harness || got.Path != targets(skill)[i].path || got.Status != want {
				t.Fatalf("skill status[%d] = %+v, want %s at %s", i, got, want, targets(skill)[i].path)
			}
		}
	}

	check("workflow", "missing")
	check("management", "missing")
	if _, _, stderr := h.run("", "skill", "show"); !strings.Contains(stderr, "requires management or workflow") {
		t.Fatalf("skill show without selector lacks usage guidance: %q", stderr)
	}
	show := h.ok("skill", "show", "workflow")
	if show != workflow.Skill {
		t.Fatal("skill show differs from embedded content")
	}
	managementShow := h.ok("skill", "show", "management")
	if managementShow != management.Skill || !strings.Contains(managementShow, "name: agent-board-management") {
		t.Fatal("management show differs from embedded content or lacks frontmatter")
	}
	legacy := filepath.Join(home, ".codex", "skills", "agent-board-workflow", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(show), 0o644); err != nil {
		t.Fatal(err)
	}
	install := h.ok("skill", "install", "workflow")
	check("workflow", "current")
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed legacy path remains: %v", err)
	}
	if repeated := h.ok("skill", "install", "workflow"); repeated != install {
		t.Fatalf("repeat install differs:\nfirst %s\nnext %s", install, repeated)
	}
	for _, target := range targets("workflow") {
		contents, err := os.ReadFile(target.path)
		if err != nil || string(contents) != show {
			t.Fatalf("installed %s skill mismatch: %v", target.harness, err)
		}
	}
	if err := os.WriteFile(targets("workflow")[1].path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := decodeJSON[struct {
		Skills []struct {
			Status []struct {
				Status string `json:"status"`
			} `json:"status"`
		} `json:"skills"`
	}](t, h.ok("skill", "check", "workflow"))
	if result.Skills[0].Status[0].Status != "current" || result.Skills[0].Status[1].Status != "different" {
		t.Fatalf("different content status = %+v", result.Skills[0].Status)
	}
	h.ok("skill", "install", "workflow")
	result = decodeJSON[struct {
		Skills []struct {
			Status []struct {
				Status string `json:"status"`
			} `json:"status"`
		} `json:"skills"`
	}](t, h.ok("skill", "check", "workflow"))
	if result.Skills[0].Status[1].Status != "different" {
		t.Fatalf("install overwrote unmanaged content: %+v", result.Skills[0].Status)
	}
	h.ok("skill", "install", "--force", "workflow")
	check("workflow", "current")
	if err := os.WriteFile(targets("workflow")[1].path, []byte(show), 0o644); err != nil {
		t.Fatal(err)
	}
	check("workflow", "current")
	h.fails(ExitUsage, CodeUsage, "skill", "show", "extra")
	all := h.ok("skill", "install")
	if !strings.Contains(all, `"skill": "management"`) || !strings.Contains(all, `"skill": "workflow"`) {
		t.Fatalf("default install did not include both: %s", all)
	}
	check("management", "current")
}

func TestSkillInstallForceIsExplicitAndScoped(t *testing.T) {
	home := t.TempDir()
	h := &harness{t: t, env: Env{Home: home}}
	h.ok("skill", "install")
	managementPath := filepath.Join(home, ".agents", "skills", "agent-board-management", "SKILL.md")
	workflowPath := filepath.Join(home, ".agents", "skills", "agent-board-workflow", "SKILL.md")
	linkedContent := filepath.Join(t.TempDir(), "user-owned-skill.md")
	if err := os.WriteFile(managementPath, []byte("user management content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(linkedContent, []byte("user workflow content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(workflowPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkedContent, workflowPath); err != nil {
		t.Fatal(err)
	}

	h.ok("skill", "install")
	for path, want := range map[string]string{managementPath: "user management content", workflowPath: "user workflow content"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("default install changed %s to %q: %v", path, got, err)
		}
	}

	forcedWorkflow := h.ok("skill", "install", "--force", "workflow")
	if repeated := h.ok("skill", "install", "workflow", "--force"); repeated != forcedWorkflow {
		t.Fatalf("repeat forced workflow install differs:\nfirst %s\nnext %s", forcedWorkflow, repeated)
	}
	managementBeforeForce, err := os.ReadFile(managementPath)
	if err != nil || string(managementBeforeForce) != "user management content" {
		t.Fatalf("forcing workflow changed management content: %q, %v", managementBeforeForce, err)
	}
	workflowContent, err := os.ReadFile(workflowPath)
	if err != nil || string(workflowContent) != workflow.Skill {
		t.Fatalf("forced workflow content differs from embedded Skill: %v", err)
	}
	linked, err := os.ReadFile(linkedContent)
	if err != nil || string(linked) != "user workflow content" {
		t.Fatalf("forcing workflow changed symlink target content: %q, %v", linked, err)
	}
	workflowCheck := h.ok("skill", "check", "workflow")
	if !strings.Contains(workflowCheck, `"status": "current"`) {
		t.Fatalf("workflow is not current after force: %s", workflowCheck)
	}
	managementCheck := h.ok("skill", "check", "management")
	if !strings.Contains(managementCheck, `"status": "different"`) {
		t.Fatalf("unselected management Skill did not remain different: %s", managementCheck)
	}

	h.ok("skill", "install", "--force", "management")
	managementContent, err := os.ReadFile(managementPath)
	if err != nil || string(managementContent) != management.Skill {
		t.Fatalf("forced management content differs from embedded Skill: %v", err)
	}
	if err := os.WriteFile(managementPath, []byte("stale management"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte("stale workflow"), 0o644); err != nil {
		t.Fatal(err)
	}
	allForced := h.ok("skill", "install", "--force")
	if !strings.Contains(allForced, `"skill": "management"`) || !strings.Contains(allForced, `"skill": "workflow"`) {
		t.Fatalf("force without selector did not include both Skills: %s", allForced)
	}
	managementContent, err = os.ReadFile(managementPath)
	if err != nil || string(managementContent) != management.Skill {
		t.Fatalf("force without selector did not update management: %v", err)
	}
	workflowContent, err = os.ReadFile(workflowPath)
	if err != nil || string(workflowContent) != workflow.Skill {
		t.Fatalf("force without selector did not update workflow: %v", err)
	}
	allCheck := h.ok("skill", "check")
	if strings.Contains(allCheck, `"status": "different"`) || strings.Contains(allCheck, `"status": "missing"`) {
		t.Fatalf("all Skills are not current after force without selector: %s", allCheck)
	}
}

func TestSkillInstallAndCheckPreserveUnmanagedSkillFiles(t *testing.T) {
	home := t.TempDir()
	h := &harness{t: t, env: Env{Home: home}}
	shared := filepath.Join(home, ".agents", "skills", "agent-board-workflow", "SKILL.md")
	legacy := filepath.Join(home, ".codex", "skills", "agent-board-workflow", "SKILL.md")
	for _, path := range []string{shared, legacy} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("user-owned"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	install := h.ok("skill", "install")
	for _, path := range []string{shared, legacy} {
		contents, err := os.ReadFile(path)
		if err != nil || string(contents) != "user-owned" {
			t.Fatalf("unmanaged content at %s changed: %q, %v", path, contents, err)
		}
		if !strings.Contains(install, path) || !strings.Contains(install, "different") || !strings.Contains(install, "unmanaged-legacy") {
			t.Fatalf("install did not report unmanaged path %s: %s", path, install)
		}
	}
	check := h.ok("skill", "check")
	if !strings.Contains(check, legacy) || !strings.Contains(check, "unmanaged-legacy") {
		t.Fatalf("check did not report unmanaged legacy path: %s", check)
	}
	code, doctor, stderr := h.run("", "doctor")
	assertDoctorEnglish(t, doctor)
	if code != ExitError || stderr != "" || !strings.Contains(doctor, legacy) || !strings.Contains(doctor, "move or merge it manually") {
		t.Fatalf("doctor did not report unmanaged legacy path and next step: %d %q %q", code, doctor, stderr)
	}
}

func TestDoctorReportsHealthyAndMissingComponentsWithoutWriting(t *testing.T) {
	h := newHarness(t)
	h.ok("skill", "install")
	output := h.ok("doctor")
	assertDoctorEnglish(t, output)
	for _, want := range []string{"Binary   OK", "Skills   OK", "Management Skill — guides Task definition and READY prioritization (CLI; MCP optional)", "Workflow Skill — guides Task execution, delivery, and verification", "Project  OK", "Board    PRESENT", "database file exists and has a recognizable SQLite format", "MCP      OK", "operation definitions and input schemas loaded (MCP service not started)"} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output %q does not contain %q", output, want)
		}
	}
	for _, unwanted := range []string{"input schema is unavailable", "project binding", "backup", "migration", "SQLite file header"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("normal doctor output exposes internal warning %q: %q", unwanted, output)
		}
	}

	database := filepath.Join(h.env.Home, ".agent-board", decodeJSON[workspace.Status](t, h.ok("check")).ProjectID, "board.db")
	before := snapshotTree(t, h.env.Home, h.repo)
	if code, out, stderr := h.run("", "doctor"); code != ExitOK || stderr != "" || out != output {
		t.Fatalf("repeat doctor exit %d stderr %q output differs", code, stderr)
	}
	if after := snapshotTree(t, h.env.Home, h.repo); !reflect.DeepEqual(before, after) {
		t.Fatalf("doctor changed project or HOME files: before %v after %v", before, after)
	}
	codexSkill := filepath.Join(h.env.Home, ".agents", "skills", "agent-board-workflow", "SKILL.md")
	claudeSkill := filepath.Join(h.env.Home, ".claude", "skills", "agent-board-workflow", "SKILL.md")
	if err := os.Remove(claudeSkill); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := h.run("", "doctor")
	assertDoctorEnglish(t, out)
	if code != ExitError || stderr != "" || !strings.Contains(out, "Skills   MISSING") || !strings.Contains(out, "Workflow Skill — guides Task execution, delivery, and verification; 1 of 3 harnesses are current") || !strings.Contains(out, "claude-code=missing") || !strings.Contains(out, "opencode=missing") || !strings.Contains(out, "2 harnesses are missing") || !strings.Contains(out, "Next step:") {
		t.Fatalf("missing Skill doctor exit %d stdout %q stderr %q", code, out, stderr)
	}
	h.ok("skill", "install")
	if err := os.WriteFile(codexSkill, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	before = snapshotTree(t, h.env.Home, h.repo)
	code, out, stderr = h.run("", "doctor")
	assertDoctorEnglish(t, out)
	if code != ExitError || stderr != "" || !strings.Contains(out, "Skills   PROBLEM") || !strings.Contains(out, "codex=different") || !strings.Contains(out, "1 installed copy has different content") || strings.Contains(out, "missing") || !strings.Contains(out, "Different installed Skill content is preserved by default") || !strings.Contains(out, "aboard skill install --force workflow") || !strings.Contains(out, "Next step:") {
		t.Fatalf("different Skill doctor exit %d stdout %q stderr %q", code, out, stderr)
	}
	if after := snapshotTree(t, h.env.Home, h.repo); !reflect.DeepEqual(before, after) {
		t.Fatalf("doctor changed files while reporting Skill mismatch: before %v after %v", before, after)
	}
	if err := os.Remove(claudeSkill); err != nil {
		t.Fatal(err)
	}
	code, out, stderr = h.run("", "doctor")
	assertDoctorEnglish(t, out)
	if code != ExitError || stderr != "" || !strings.Contains(out, "Skills   PROBLEM") || !strings.Contains(out, "0 of 3 harnesses are current") || !strings.Contains(out, "1 installed copy has different content") || !strings.Contains(out, "2 harnesses are missing") || !strings.Contains(out, "codex=different") || !strings.Contains(out, "claude-code=missing") || !strings.Contains(out, "opencode=missing") {
		t.Fatalf("mixed Skill doctor exit %d stdout %q stderr %q", code, out, stderr)
	}
	h.ok("skill", "install")
	if err := os.WriteFile(codexSkill, []byte(workflow.Skill), 0o644); err != nil {
		t.Fatal(err)
	}
	garbage := []byte("GARBAGE NOT A SQLITE FILE")
	if err := os.WriteFile(database, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	before = snapshotTree(t, h.env.Home, h.repo)
	code, out, stderr = h.run("", "doctor")
	assertDoctorEnglish(t, out)
	if code != ExitError || stderr != "" || !strings.Contains(out, "Board    PROBLEM") || !strings.Contains(out, "does not have a valid SQLite file header") || !strings.Contains(out, "Next step:") {
		t.Fatalf("garbage database doctor exit %d stdout %q stderr %q", code, out, stderr)
	}
	if after := snapshotTree(t, h.env.Home, h.repo); !reflect.DeepEqual(before, after) {
		t.Fatalf("doctor changed files while reporting corrupt DB: before %v after %v", before, after)
	}

	if err := os.Remove(database); err != nil {
		t.Fatal(err)
	}
	before = snapshotTree(t, h.env.Home, h.repo)
	code, out, stderr = h.run("", "doctor")
	assertDoctorEnglish(t, out)
	if code != ExitError || stderr != "" || !strings.Contains(out, "Board    MISSING") || !strings.Contains(out, "Next step:") {
		t.Fatalf("missing database doctor exit %d stdout %q stderr %q", code, out, stderr)
	}
	if after := snapshotTree(t, h.env.Home, h.repo); !reflect.DeepEqual(before, after) {
		t.Fatalf("doctor changed files while reporting missing DB: before %v after %v", before, after)
	}
}

func TestDoctorOutsideProjectAndWithMissingSkill(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	h := &harness{t: t, repo: dir, env: Env{Dir: dir, Home: home, Version: "test"}}
	code, output, stderr := h.run("", "doctor")
	if code != ExitError || stderr != "" {
		t.Fatalf("doctor exit %d stdout %q stderr %q", code, output, stderr)
	}
	for _, want := range []string{"Binary   OK", "Skills   MISSING", "Management Skill", "Workflow Skill", "Project  MISSING", "Board    MISSING", "MCP      OK", "aboard init", "aboard skill install"} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output %q does not contain %q", output, want)
		}
	}
	assertDoctorEnglish(t, output)
	for _, path := range []string{filepath.Join(home, ".agent-board"), filepath.Join(home, ".claude"), filepath.Join(home, ".codex"), filepath.Join(home, ".agents")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("doctor created %s: %v", path, err)
		}
	}
	h.fails(ExitUsage, CodeUsage, "doctor", "unexpected")
}

func TestDoctorReportsInvalidProjectConfigurationWithoutWriting(t *testing.T) {
	h := newHarness(t)
	identity := localIdentity(h.repo)
	if err := os.WriteFile(identity, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, h.env.Home, h.repo)
	code, output, stderr := h.run("", "doctor")
	assertDoctorEnglish(t, output)
	if code != ExitError || stderr != "" || !strings.Contains(output, "Project  PROBLEM") || !strings.Contains(output, "Board    PROBLEM") || !strings.Contains(output, "Fix the project identity issue reported under Project first.") || !strings.Contains(output, "Next step:") {
		t.Fatalf("invalid project doctor exit %d stdout %q stderr %q", code, output, stderr)
	}
	if after := snapshotTree(t, h.env.Home, h.repo); !reflect.DeepEqual(before, after) {
		t.Fatalf("doctor changed files while reporting invalid project config: before %v after %v", before, after)
	}
}

func TestMCPConfigCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent board binary")
	h := &harness{t: t, env: Env{
		ExecutablePath: func() (string, error) { return path, nil },
	}}
	checks := []struct {
		harness  string
		contains string
		actor    string
	}{
		{"generic", `"args": [`, "harness/generic"},
		{"claude-code", `claude mcp add --transport stdio --scope user aboard --env 'AGENT_BOARD_ACTOR=harness/claude-code' -- '`, "harness/claude-code"},
		{"codex", `[mcp_servers.aboard]`, "harness/codex"},
		{"opencode", `"aboard": {`, "harness/opencode"},
	}
	for _, check := range checks {
		got := h.ok("mcp", "config", check.harness)
		if !strings.Contains(got, check.contains) || !strings.Contains(got, path) || !strings.Contains(got, check.actor) {
			t.Errorf("%s config = %q; want marker %q and executable %q", check.harness, got, check.contains, path)
		}
	}
	generic := h.ok("mcp", "config")
	var genericConfig struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(generic), &genericConfig); err != nil {
		t.Fatalf("generic config is not JSON: %v (%q)", err, generic)
	}
	if genericConfig.Command != path || !filepath.IsAbs(genericConfig.Command) || !reflect.DeepEqual(genericConfig.Args, []string{"mcp"}) || genericConfig.Env["AGENT_BOARD_ACTOR"] != "harness/generic" {
		t.Fatalf("generic MCP config = %+v", genericConfig)
	}
	claude := h.ok("mcp", "config", "claude-code")
	if !strings.Contains(claude, "--transport stdio") || !strings.Contains(claude, "aboard --env 'AGENT_BOARD_ACTOR=harness/claude-code' -- '") {
		t.Fatalf("Claude Code config does not carry stdio actor environment: %q", claude)
	}
	codex := h.ok("mcp", "config", "codex")
	if !strings.Contains(codex, `args = ["mcp"]`) || !strings.Contains(codex, `env = { AGENT_BOARD_ACTOR = "harness/codex" }`) {
		t.Fatalf("Codex config does not carry stdio actor environment: %q", codex)
	}
	opencode := h.ok("mcp", "config", "opencode")
	var openCodeConfig struct {
		MCP map[string]struct {
			Type        string            `json:"type"`
			Command     []string          `json:"command"`
			Environment map[string]string `json:"environment"`
			Enabled     bool              `json:"enabled"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal([]byte(opencode[strings.Index(opencode, "{"):]), &openCodeConfig); err != nil {
		t.Fatalf("OpenCode config is not JSON: %v (%q)", err, opencode)
	}
	openCodeServer := openCodeConfig.MCP["aboard"]
	if openCodeServer.Type != "local" || !reflect.DeepEqual(openCodeServer.Command, []string{path, "mcp"}) || openCodeServer.Environment["AGENT_BOARD_ACTOR"] != "harness/opencode" || !openCodeServer.Enabled {
		t.Fatalf("OpenCode MCP config = %+v", openCodeServer)
	}
	h.fails(ExitUsage, CodeUsage, "mcp", "config", "unknown")
	h.fails(ExitUsage, CodeUsage, "mcp", "config", "codex", "extra")
	h.env.ExecutablePath = func() (string, error) { return "relative/aboard", nil }
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
	h.ok("fact", "record", "1", "--kind", "decision", "--body", "Human accepts without verifier PASS", "--data", `{"decision":"accept"}`)
	code, _, stderr := h.run("delivery evidence\n", "fact", "record", "1", "--kind", "delivery", "--body-file", "-", "--actor", "developer", "--role", "worker", "--session", "cli-session", "--harness", "codex", "--model", "gpt-6")
	if code != ExitOK {
		t.Fatal(stderr)
	}
	facts := decodeJSON[ops.FactsResult](t, h.ok("fact", "list", "1", "--kind", "delivery")).Facts
	if len(facts) != 1 || facts[0].Body != "delivery evidence\n" || facts[0].Actor != "developer" || facts[0].Provenance == nil || facts[0].Provenance.Session != "cli-session" || facts[0].Provenance.Harness != "codex" {
		t.Fatalf("facts %+v", facts)
	}

	history := decodeJSON[struct {
		Task   domain.Task        `json:"task"`
		Facts  []domain.TaskFact  `json:"facts"`
		Events []domain.TaskEvent `json:"events"`
	}](t, h.ok("history", "1"))
	if len(history.Facts) != 3 || len(history.Events) != 7 || history.Task.Number != 1 {
		t.Fatalf("history %d facts %d events", len(history.Facts), len(history.Events))
	}
	page := decodeJSON[ops.EventsResult](t, h.ok("events", "--after", "2", "--limit", "3"))
	if len(page.Events) != 3 || page.Events[0].ID != 3 {
		t.Fatalf("page %+v", page)
	}

	view := decodeJSON[struct {
		ProjectID   string            `json:"project_id"`
		ProjectName string            `json:"project_name"`
		Ready       domain.ReadyQueue `json:"ready"`
		Tasks       []domain.Task     `json:"tasks"`
	}](t, h.ok("board"))
	identity := decodeJSON[projectconfig.Identity](t, string(mustRead(t, localIdentity(h.repo))))
	if len(view.Tasks) != 2 || len(view.Ready.Tasks) != 1 || view.ProjectID != identity.ProjectID || view.ProjectName != identity.Name {
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

	if code, stdout, _ := h.run("", "help"); code != ExitOK || !strings.Contains(stdout, "aboard") {
		t.Fatalf("help exit %d", code)
	}
}

func TestTaskCreateExplainsHowToSetMissingActor(t *testing.T) {
	h := newHarness(t)
	h.env.Actor = ""

	code, stdout, stderr := h.run("", "task", "create", "--title", "x")
	if code != ExitError || stdout != "" {
		t.Fatalf("task create exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	var envelope struct {
		Error domain.Error `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &envelope); err != nil {
		t.Fatalf("stderr is not a JSON error: %q: %v", stderr, err)
	}
	if envelope.Error.Code != domain.CodeInvalidArgument {
		t.Fatalf("error code %q, want %s", envelope.Error.Code, domain.CodeInvalidArgument)
	}
	for _, hint := range []string{"--actor LABEL", "AGENT_BOARD_ACTOR"} {
		if !strings.Contains(envelope.Error.Message, hint) {
			t.Fatalf("error message %q does not contain %q", envelope.Error.Message, hint)
		}
	}
	if tasks := decodeJSON[ops.TasksResult](t, h.ok("task", "list")); len(tasks.Tasks) != 0 {
		t.Fatalf("failed create left tasks: %+v", tasks.Tasks)
	}

	h.ok("task", "create", "--title", "explicit", "--actor", "human/test")
	h.env.Actor = "human/env"
	h.ok("task", "create", "--title", "from environment")
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
	identity := decodeJSON[projectconfig.Identity](t, string(mustRead(t, localIdentity(h.repo))))
	instructions := session.InitializeResult().Instructions
	for _, want := range []string{identity.Name, identity.ProjectID, filepath.Join(h.env.Home, ".agent-board", identity.ProjectID, "board.db")} {
		if !strings.Contains(instructions, want) {
			t.Errorf("MCP initialize instructions %q do not contain %q", instructions, want)
		}
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

type webStart struct {
	URL       string `json:"url"`
	ProjectID string `json:"project_id"`
	Actor     string `json:"actor"`
}

// startWeb runs `web args...` against the harness project and returns the
// start line it printed. The server stops when the test ends.
func (h *harness) startWeb(args ...string) webStart {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	env := h.env
	env.Stdin, env.Stdout, env.Stderr = strings.NewReader(""), writer, io.Discard
	done := make(chan int, 1)
	go func() { done <- Run(ctx, append([]string{"web"}, args...), env) }()
	h.t.Cleanup(func() {
		cancel()
		if code := <-done; code != ExitOK {
			h.t.Errorf("web exited %d", code)
		}
	})
	var started webStart
	if err := json.NewDecoder(reader).Decode(&started); err != nil {
		h.t.Fatal(err)
	}
	return started
}

func (h *harness) getPage(url string) string {
	h.t.Helper()
	response, err := http.Get(url)
	if err != nil {
		h.t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		h.t.Fatalf("GET %s: status %d", url, response.StatusCode)
	}
	return string(body)
}

// loopbackPort returns the non-zero port of a started web URL, requiring a
// loopback host.
func loopbackPort(t *testing.T, started webStart) string {
	t.Helper()
	parsed, err := neturl.Parse(started.URL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.Port() == "0" {
		t.Fatalf("url %q is not a loopback URL with a real port", started.URL)
	}
	return parsed.Port()
}

func TestWebCommand(t *testing.T) {
	h := newHarness(t)
	h.fails(ExitUsage, CodeUsage, "web", "--addr", "0.0.0.0:7420")
	h.fails(ExitUsage, CodeUsage, "web", "--addr", "localhost:7420")

	h.env.Actor = ""
	started := h.startWeb("--addr", "127.0.0.1:0")
	loopbackPort(t, started)
	if started.Actor != DefaultWebActor || started.ProjectID == "" {
		t.Fatalf("started %+v", started)
	}
	if body := h.getPage(started.URL); !strings.Contains(body, started.ProjectID) {
		t.Fatal("page does not show the project")
	}
}

// Without --addr the Web Board binds a free loopback port chosen by the OS,
// so two projects can serve at once and each shows only its own Board.
func TestWebDefaultAddressIsDynamicAndPerProject(t *testing.T) {
	first, second := newHarness(t), newHarness(t)
	first.ok("task", "create", "--title", "alpha-only-task")
	second.ok("task", "create", "--title", "beta-only-task")

	a, b := first.startWeb(), second.startWeb()
	if loopbackPort(t, a) == loopbackPort(t, b) || a.URL == b.URL {
		t.Fatalf("both projects share an address: %s %s", a.URL, b.URL)
	}
	if a.ProjectID == b.ProjectID {
		t.Fatalf("projects share an identity %s", a.ProjectID)
	}
	pageA, pageB := first.getPage(a.URL), second.getPage(b.URL)
	if !strings.Contains(pageA, "alpha-only-task") || strings.Contains(pageA, "beta-only-task") {
		t.Fatal("first Board is not isolated")
	}
	if !strings.Contains(pageB, "beta-only-task") || strings.Contains(pageB, "alpha-only-task") {
		t.Fatal("second Board is not isolated")
	}
}

// An explicit --addr keeps its meaning: the named port is bound, and a taken
// port is an error rather than a silent move to another port.
func TestWebExplicitAddress(t *testing.T) {
	h := newHarness(t)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(probe.Addr().(*net.TCPAddr).Port)
	probe.Close()

	started := h.startWeb("--addr", "127.0.0.1:"+port)
	if started.URL != "http://127.0.0.1:"+port+"/" {
		t.Fatalf("url %q, want port %s", started.URL, port)
	}
	h.getPage(started.URL)

	code, stdout, stderr := h.run("", "web", "--addr", "127.0.0.1:"+port)
	if code == ExitOK || stdout != "" || !strings.Contains(stderr, "address already in use") {
		t.Fatalf("occupied port: exit %d stdout %q stderr %q", code, stdout, stderr)
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
	for _, dir := range []string{".", "../mcpserver", "../ops", "../web", "../../cmd/aboard"} {
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
