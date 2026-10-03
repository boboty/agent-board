package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/boboty/agent-board/internal/board"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/projectconfig"
)

func testHome(t *testing.T) string {
	t.Helper()
	return realDir(t, t.TempDir())
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
	home := testHome(t)
	repo := realDir(t, t.TempDir())
	git(t, repo, "init", "-q")

	created, err := Init(ctx, repo, home, filepath.Base(repo))
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	located, err := Locate(nested, home)
	if err != nil {
		t.Fatal(err)
	}
	if located.Root != repo || located.Identity != created.Identity {
		t.Fatalf("located %+v, created %+v", located, created)
	}
	if located.DatabasePath != created.DatabasePath || located.DataDir != created.DataDir {
		t.Fatalf("located database %s, created %s", located.DatabasePath, created.DatabasePath)
	}
	if want := filepath.Join(home, ".agent-board", created.Identity.ProjectID, "board.db"); located.DatabasePath != want {
		t.Fatalf("database %s, want %s", located.DatabasePath, want)
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

	if _, err := Init(ctx, repo, home, filepath.Base(repo)); !domain.IsCode(err, projectconfig.CodeProjectAlreadyInitialized) {
		t.Fatalf("second init: %v", err)
	}
}

func TestLocateWithoutIdentity(t *testing.T) {
	_, err := Locate(t.TempDir(), testHome(t))
	if !domain.IsCode(err, projectconfig.CodeProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

// A local identity whose data directory is missing (e.g. after the data was
// removed) is reported by Check without creating anything; Open creates it.
func TestMissingDataDirectoryCreatedOnOpen(t *testing.T) {
	ctx := context.Background()
	home := testHome(t)
	clone := t.TempDir()
	git(t, clone, "init", "-q")
	identity := `{"version": 2, "project_id": "01M3VN4DT676SGJ90T58JRB13R", "name": "fixture"}`
	if err := os.WriteFile(filepath.Join(clone, ".git", projectconfig.IdentityFileName), []byte(identity), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := Locate(clone, home)
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

// Two git worktrees of one repository share the identity in the Git common
// directory, with nothing committed, and so resolve, open, and see one Board.
func TestWorktreesShareOneBoard(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ctx := context.Background()
	home := testHome(t)
	base := realDir(t, t.TempDir())
	repo := filepath.Join(base, "repo")
	worktree := filepath.Join(base, "worktree")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	if _, err := Init(ctx, repo, home, filepath.Base(repo)); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "base")
	git(t, repo, "worktree", "add", "-q", worktree)

	main, err := Locate(repo, home)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Locate(worktree, home)
	if err != nil {
		t.Fatal(err)
	}
	if other.Root != worktree || main.DatabasePath != other.DatabasePath {
		t.Fatalf("worktree root %s database %s, main database %s", other.Root, other.DatabasePath, main.DatabasePath)
	}
	if main.Identity.Name == "" || main.Identity.Name != other.Identity.Name {
		t.Fatalf("main name %q and worktree name %q do not match", main.Identity.Name, other.Identity.Name)
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
