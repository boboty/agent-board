package projectconfig_test

import (
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/boboty/agent-board/internal/clock"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ids"
	"github.com/boboty/agent-board/internal/projectconfig"
)

func generator(t *testing.T) *ids.Generator {
	t.Helper()
	g, err := ids.NewGenerator(clock.RealClock{}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitRepo returns a fresh, symlink-resolved Git repository.
func gitRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	return repo
}

func localIdentity(repo string) string {
	return filepath.Join(repo, ".git", projectconfig.IdentityFileName)
}

func TestInitializeAndDiscover(t *testing.T) {
	repo, dataRoot := gitRepo(t), t.TempDir()
	project, err := projectconfig.Initialize(repo, generator(t), dataRoot, "test project")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(project.DataDir); err != nil {
		t.Fatalf("data dir not created: %v", err)
	}
	want, _ := projectconfig.ProjectDatabasePath(dataRoot, project.Identity.ProjectID)
	if project.DatabasePath != want {
		t.Fatalf("DatabasePath = %s, want %s", project.DatabasePath, want)
	}
	if project.IdentityPath != localIdentity(repo) {
		t.Fatalf("IdentityPath = %s", project.IdentityPath)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agent-board.json")); !os.IsNotExist(err) {
		t.Fatal("Initialize wrote an identity into the worktree")
	}

	nested := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := projectconfig.Discover(nested)
	if err != nil || found.Identity != project.Identity || found.Root != repo {
		t.Fatalf("Discover() = %+v, %v", found, err)
	}

	_, err = projectconfig.Initialize(repo, generator(t), dataRoot, "test project")
	if !domain.IsCode(err, projectconfig.CodeProjectAlreadyInitialized) {
		t.Fatalf("re-Initialize() error = %v", err)
	}
}

func TestInitializeRequiresGitRepository(t *testing.T) {
	if _, err := projectconfig.Initialize(t.TempDir(), generator(t), t.TempDir(), "x"); !domain.IsCode(err, projectconfig.CodeProjectNotFound) {
		t.Fatalf("Initialize() outside Git error = %v", err)
	}
}

func TestInitializeRejectsDataRootInsideRepository(t *testing.T) {
	repo := gitRepo(t)
	_, err := projectconfig.Initialize(repo, generator(t), filepath.Join(repo, ".board-data"), "test project")
	if !domain.IsCode(err, domain.CodeStorageConfiguration) {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := os.Stat(localIdentity(repo)); !os.IsNotExist(err) {
		t.Fatal("rejected Initialize wrote an identity file")
	}
}

func TestDiscoverRejectsInvalidIdentity(t *testing.T) {
	for name, contents := range map[string]string{
		"bad id":        `{"version":2,"project_id":"nope","name":"test"}`,
		"unknown field": `{"version":2,"project_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","name":"test","x":1}`,
		"version":       `{"version":3,"project_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","name":"test"}`,
		"version 1":     `{"version":1,"project_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`,
		"missing name":  `{"version":2,"project_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`,
		"empty name":    `{"version":2,"project_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","name":"  "}`,
	} {
		repo := gitRepo(t)
		if err := os.WriteFile(localIdentity(repo), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := projectconfig.Discover(repo); !domain.IsCode(err, projectconfig.CodeInvalidIdentity) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	if _, err := projectconfig.Discover(gitRepo(t)); !domain.IsCode(err, projectconfig.CodeProjectNotFound) {
		t.Errorf("missing identity: error = %v", err)
	}
	if _, err := projectconfig.Discover(t.TempDir()); !domain.IsCode(err, projectconfig.CodeProjectNotFound) {
		t.Errorf("outside Git: error = %v", err)
	}
}

func TestResolveDataRoot(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "u")
	got, err := projectconfig.ResolveDataRoot(home)
	if want := filepath.Join(home, ".agent-board"); err != nil || got != want {
		t.Errorf("ResolveDataRoot(%q) = %q, %v; want %q", home, got, err, want)
	}
	if _, err := projectconfig.ResolveDataRoot(""); !domain.IsCode(err, projectconfig.CodePathResolution) {
		t.Errorf("empty home: error = %v", err)
	}
}

func TestProjectDatabasePath(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "home", "u", ".agent-board")
	const id = "01M3VN4DT676SGJ90T58JRB13R"
	got, err := projectconfig.ProjectDatabasePath(root, id)
	if want := filepath.Join(root, id, "board.db"); err != nil || got != want {
		t.Errorf("ProjectDatabasePath() = %q, %v; want %q", got, err, want)
	}
	if _, err := projectconfig.ProjectDatabasePath(root, "nope"); !domain.IsCode(err, projectconfig.CodeInvalidIdentity) {
		t.Errorf("bad id: error = %v", err)
	}
}

// Worktrees share the Git common directory and therefore one identity, with
// nothing committed. An independent clone gets no identity at all.
func TestWorktreesShareIdentityAndClonesDoNot(t *testing.T) {
	repo, dataRoot := gitRepo(t), t.TempDir()
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "base")
	project, err := projectconfig.Initialize(repo, generator(t), dataRoot, "worktree project")
	if err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-wt")
	git(t, repo, "worktree", "add", "-q", worktree)
	t.Cleanup(func() { os.RemoveAll(worktree) })
	found, err := projectconfig.Discover(worktree)
	if err != nil || found.Identity != project.Identity || found.IdentityPath != project.IdentityPath {
		t.Fatalf("worktree Discover() = %+v, %v", found, err)
	}

	clone := filepath.Join(t.TempDir(), "clone")
	git(t, repo, "clone", "-q", repo, clone)
	if _, err := projectconfig.Discover(clone); !domain.IsCode(err, projectconfig.CodeProjectNotFound) {
		t.Fatalf("clone Discover() error = %v", err)
	}
}

func TestInitializeRejectsInvalidName(t *testing.T) {
	for _, name := range []string{"", " \t ", "bad\nname"} {
		repo := gitRepo(t)
		if _, err := projectconfig.Initialize(repo, generator(t), t.TempDir(), name); !domain.IsCode(err, projectconfig.CodeInitializationFailed) {
			t.Errorf("Initialize(%q) error = %v", name, err)
		}
		if _, err := os.Stat(localIdentity(repo)); !os.IsNotExist(err) {
			t.Errorf("Initialize(%q) wrote identity", name)
		}
	}
}
