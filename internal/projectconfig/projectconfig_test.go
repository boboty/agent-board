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

	nested := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := projectconfig.Discover(nested)
	if err != nil || found.Identity != project.Identity {
		t.Fatalf("Discover() = %+v, %v", found, err)
	}

	_, err = projectconfig.Initialize(repo, generator(t), dataRoot, "test project")
	if !domain.IsCode(err, projectconfig.CodeProjectAlreadyInitialized) {
		t.Fatalf("re-Initialize() error = %v", err)
	}
}

func TestInitializeRejectsDataRootInsideRepository(t *testing.T) {
	repo := t.TempDir()
	_, err := projectconfig.Initialize(repo, generator(t), filepath.Join(repo, ".board-data"), "test project")
	if !domain.IsCode(err, domain.CodeStorageConfiguration) {
		t.Fatalf("Initialize() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, projectconfig.IdentityFileName)); !os.IsNotExist(err) {
		t.Fatal("rejected Initialize wrote an identity file")
	}
}

func TestDiscoverRejectsInvalidIdentity(t *testing.T) {
	for name, contents := range map[string]string{
		"bad id":        `{"version":2,"project_id":"nope","name":"test"}`,
		"unknown field": `{"version":2,"project_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","name":"test","x":1}`,
		"version":       `{"version":3,"project_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","name":"test"}`,
		"missing name":  `{"version":2,"project_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`,
		"empty name":    `{"version":2,"project_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","name":"  "}`,
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

// TestWorktreesShareOneBoard models two worktrees of one repository: each
// carries the same committed identity file, so both resolve to the same
// external database and see each other's writes.
func TestWorktreesShareOneBoard(t *testing.T) {
	main, dataRoot := t.TempDir(), t.TempDir()
	project, err := projectconfig.Initialize(main, generator(t), dataRoot, "worktree project")
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

func TestVersionOneRequiresExplicitMigration(t *testing.T) {
	root, dataRoot := t.TempDir(), t.TempDir()
	const id = "01M3VN4DT676SGJ90T58JRB13R"
	identityPath := filepath.Join(root, projectconfig.IdentityFileName)
	legacy := []byte(`{"version":1,"project_id":"` + id + `"}`)
	if err := os.WriteFile(identityPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := projectconfig.Discover(root); !domain.IsCode(err, projectconfig.CodeIdentityMigrationRequired) {
		t.Fatalf("Discover v1 error = %v", err)
	}
	inspected, err := projectconfig.Inspect(root)
	if err != nil || inspected.Identity.Version != 1 || inspected.Identity.ProjectID != id {
		t.Fatalf("Inspect v1 = %+v, %v", inspected, err)
	}
	dataDir := filepath.Join(dataRoot, id)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := board.Open(context.Background(), board.Config{DatabasePath: filepath.Join(dataDir, "board.db"), ProjectID: id})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	migrated, err := projectconfig.MigrateIdentity(root, "Readable name")
	if err != nil || migrated.Version != 2 || migrated.ProjectID != id || migrated.Name != "Readable name" {
		t.Fatalf("MigrateIdentity = %+v, %v", migrated, err)
	}
	found, err := projectconfig.Discover(root)
	if err != nil || found.Identity != migrated {
		t.Fatalf("Discover migrated = %+v, %v", found, err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "board.db")); err != nil {
		t.Fatalf("migration changed Board data: %v", err)
	}
}

func TestInitializeRejectsInvalidName(t *testing.T) {
	for _, name := range []string{"", " \t ", "bad\nname"} {
		repo := t.TempDir()
		if _, err := projectconfig.Initialize(repo, generator(t), t.TempDir(), name); !domain.IsCode(err, projectconfig.CodeInitializationFailed) {
			t.Errorf("Initialize(%q) error = %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(repo, projectconfig.IdentityFileName)); !os.IsNotExist(err) {
			t.Errorf("Initialize(%q) wrote identity", name)
		}
	}
}
