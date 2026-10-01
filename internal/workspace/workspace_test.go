package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/boboty/agent-board/internal/board"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/projectconfig"
)

func testInputs(t *testing.T) projectconfig.PathInputs {
	t.Helper()
	home := realDir(t, t.TempDir())
	return projectconfig.PathInputs{GOOS: runtime.GOOS, HomeDir: home, XDGDataHome: filepath.Join(home, "xdg"), LocalAppData: home}
}

func realDir(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestInitThenLocateFromNestedDirectory(t *testing.T) {
	ctx := context.Background()
	inputs := testInputs(t)
	repo := realDir(t, t.TempDir())

	created, err := Init(ctx, repo, inputs)
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	located, err := Locate(nested, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if located.Root != repo || located.Identity != created.Identity {
		t.Fatalf("located %+v, created %+v", located, created)
	}
	if located.DatabasePath != created.DatabasePath || located.DataDir != created.DataDir {
		t.Fatalf("located database %s, created %s", located.DatabasePath, created.DatabasePath)
	}
	if rel, err := filepath.Rel(repo, located.DatabasePath); err == nil && filepath.IsLocal(rel) {
		t.Fatalf("database %s is inside the repository", located.DatabasePath)
	}

	status, err := Check(ctx, located)
	if err != nil {
		t.Fatal(err)
	}
	if !status.DatabaseExists || status.SchemaVersion != 1 || status.ProjectID != created.Identity.ProjectID {
		t.Fatalf("status %+v", status)
	}

	if _, err := Init(ctx, repo, inputs); !domain.IsCode(err, projectconfig.CodeProjectAlreadyInitialized) {
		t.Fatalf("second init: %v", err)
	}
}

func TestLocateWithoutIdentity(t *testing.T) {
	_, err := Locate(t.TempDir(), testInputs(t))
	if !domain.IsCode(err, projectconfig.CodeProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

// A fresh clone on a new machine has the committed identity but no data
// directory. Check reports that without creating anything; Open creates it.
func TestFreshCloneCreatesDatabaseOnOpen(t *testing.T) {
	ctx := context.Background()
	inputs := testInputs(t)
	clone := t.TempDir()
	identity := `{"version": 1, "project_id": "01M3VN4DT676SGJ90T58JRB13R"}`
	if err := os.WriteFile(filepath.Join(clone, projectconfig.IdentityFileName), []byte(identity), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := Locate(clone, inputs)
	if err != nil {
		t.Fatal(err)
	}
	status, err := Check(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	if status.DatabaseExists {
		t.Fatal("check reported a database that was never created")
	}
	if _, err := os.Stat(project.DataDir); !os.IsNotExist(err) {
		t.Fatalf("check created the data directory: %v", err)
	}

	service, err := Open(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close(ctx)
	if _, err := os.Stat(project.DatabasePath); err != nil {
		t.Fatal(err)
	}
}

// Two git worktrees of one repository share the committed identity and so
// resolve, open, and see one Board.
func TestWorktreesShareOneBoard(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ctx := context.Background()
	inputs := testInputs(t)
	base := realDir(t, t.TempDir())
	repo := filepath.Join(base, "repo")
	worktree := filepath.Join(base, "worktree")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	if _, err := Init(ctx, repo, inputs); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", projectconfig.IdentityFileName)
	git(t, repo, "commit", "-q", "-m", "identity")
	git(t, repo, "worktree", "add", "-q", worktree)

	main, err := Locate(repo, inputs)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Locate(worktree, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if other.Root != worktree || main.DatabasePath != other.DatabasePath {
		t.Fatalf("worktree root %s database %s, main database %s", other.Root, other.DatabasePath, main.DatabasePath)
	}

	a, err := Open(ctx, main)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close(ctx)
	b, err := Open(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close(ctx)
	created, err := a.CreateTask(ctx, board.CreateTaskInput{Actor: "main", Title: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	seen, err := b.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Title != "shared" {
		t.Fatalf("worktree saw %+v", seen)
	}
}
