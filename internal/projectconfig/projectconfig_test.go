package projectconfig_test

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/boboty/agent-board/internal/board"
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

func TestInitializeAndDiscover(t *testing.T) {
	repo, dataRoot := t.TempDir(), t.TempDir()
	project, err := projectconfig.Initialize(repo, generator(t), dataRoot)
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

	nested := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := projectconfig.Discover(nested)
	if err != nil || found.Identity != project.Identity {
		t.Fatalf("Discover() = %+v, %v", found, err)
	}

	_, err = projectconfig.Initialize(repo, generator(t), dataRoot)
	if !domain.IsCode(err, projectconfig.CodeProjectAlreadyInitialized) {
		t.Fatalf("re-Initialize() error = %v", err)
	}
}

func TestInitializeRejectsDataRootInsideRepository(t *testing.T) {
	repo := t.TempDir()
	_, err := projectconfig.Initialize(repo, generator(t), filepath.Join(repo, ".board-data"))
	if !domain.IsCode(err, domain.CodeStorageConfiguration) {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, projectconfig.IdentityFileName)); !os.IsNotExist(err) {
		t.Fatal("rejected Initialize wrote an identity file")
	}
}

func TestDiscoverRejectsInvalidIdentity(t *testing.T) {
	for name, contents := range map[string]string{
		"bad id":        `{"version":1,"project_id":"nope"}`,
		"unknown field": `{"version":1,"project_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","x":1}`,
		"version":       `{"version":2,"project_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`,
	} {
		repo := t.TempDir()
		if err := os.WriteFile(filepath.Join(repo, projectconfig.IdentityFileName), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := projectconfig.Discover(repo); !domain.IsCode(err, projectconfig.CodeInvalidIdentity) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	if _, err := projectconfig.Discover(t.TempDir()); !domain.IsCode(err, projectconfig.CodeProjectNotFound) {
		t.Errorf("missing identity: error = %v", err)
	}
}

func TestResolveDataRoot(t *testing.T) {
	cases := []struct {
		in   projectconfig.PathInputs
		want string
	}{
		{projectconfig.PathInputs{GOOS: "darwin", HomeDir: "/Users/u"}, "/Users/u/Library/Application Support/agent-board"},
		{projectconfig.PathInputs{GOOS: "linux", HomeDir: "/home/u"}, "/home/u/.local/share/agent-board"},
		{projectconfig.PathInputs{GOOS: "linux", XDGDataHome: "/xdg"}, "/xdg/agent-board"},
		{projectconfig.PathInputs{GOOS: "windows", LocalAppData: `C:\Users\u\AppData\Local`}, `C:\Users\u\AppData\Local\agent-board`},
	}
	for _, c := range cases {
		if got, err := projectconfig.ResolveDataRoot(c.in); err != nil || got != c.want {
			t.Errorf("ResolveDataRoot(%+v) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := projectconfig.ResolveDataRoot(projectconfig.PathInputs{GOOS: "plan9"}); err == nil {
		t.Error("unsupported OS accepted")
	}
}

// TestWorktreesShareOneBoard models two worktrees of one repository: each
// carries the same committed identity file, so both resolve to the same
// external database and see each other's writes.
func TestWorktreesShareOneBoard(t *testing.T) {
	main, dataRoot := t.TempDir(), t.TempDir()
	project, err := projectconfig.Initialize(main, generator(t), dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := os.ReadFile(filepath.Join(main, projectconfig.IdentityFileName))
	if err != nil {
		t.Fatal(err)
	}
	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, projectconfig.IdentityFileName), identity, 0o600); err != nil {
		t.Fatal(err)
	}

	open := func(dir string) *board.Service {
		found, err := projectconfig.Discover(dir)
		if err != nil {
			t.Fatal(err)
		}
		path, err := projectconfig.ProjectDatabasePath(dataRoot, found.Identity.ProjectID)
		if err != nil {
			t.Fatal(err)
		}
		s, err := board.Open(context.Background(), board.Config{DatabasePath: path, ProjectID: found.Identity.ProjectID})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close(context.Background()) })
		return s
	}
	fromMain, fromWorktree := open(main), open(worktree)
	created, err := fromMain.CreateTask(context.Background(), board.CreateTaskInput{Actor: "orchestrator", Title: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := fromWorktree.GetTask(context.Background(), created.ID); err != nil || got.Title != "shared" {
		t.Fatalf("worktree view = %+v, %v (db %s)", got, err, project.DatabasePath)
	}
}
