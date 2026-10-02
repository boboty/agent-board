// Package workspace locates the current project's Board and opens it. Every
// worktree of a repository carries the same committed .agent-board.json, so
// every worktree, harness, and process resolves the same database outside the
// repository.
package workspace

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"

	"github.com/boboty/agent-board/internal/board"
	"github.com/boboty/agent-board/internal/clock"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ids"
	"github.com/boboty/agent-board/internal/projectconfig"
	"github.com/boboty/agent-board/migrations"
)

// HomeDir returns the current user's home directory, below which the Board
// keeps its data. It is empty when the home directory cannot be determined.
func HomeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

// Locate discovers the project identity at or above start and resolves its
// data directory and database path. It touches nothing on disk.
func Locate(start string, home string) (projectconfig.Project, error) {
	project, err := projectconfig.Discover(start)
	if err != nil {
		return projectconfig.Project{}, err
	}
	dataRoot, err := projectconfig.ResolveDataRoot(home)
	if err != nil {
		return projectconfig.Project{}, err
	}
	databasePath, err := projectconfig.ProjectDatabasePath(dataRoot, project.Identity.ProjectID)
	if err != nil {
		return projectconfig.Project{}, err
	}
	project.DatabasePath = databasePath
	project.DataDir = filepath.Dir(project.DatabasePath)
	return project, nil
}

// Open creates the project data directory when missing (a fresh clone or a
// new machine), then opens and migrates the Board bound to the project ID.
func Open(ctx context.Context, project projectconfig.Project) (*board.Service, error) {
	if err := os.MkdirAll(project.DataDir, 0o700); err != nil {
		return nil, domain.WrapError(err, domain.CodeStorageUnavailable, "cannot create project data directory "+project.DataDir, false)
	}
	return board.Open(ctx, board.Config{DatabasePath: project.DatabasePath, ProjectID: project.Identity.ProjectID})
}

// Init creates a new project identity in dir and its Board database.
func Init(ctx context.Context, dir string, home, name string) (projectconfig.Project, error) {
	dataRoot, err := projectconfig.ResolveDataRoot(home)
	if err != nil {
		return projectconfig.Project{}, err
	}
	generator, err := ids.NewGenerator(clock.RealClock{}, rand.Reader)
	if err != nil {
		return projectconfig.Project{}, err
	}
	project, err := projectconfig.Initialize(dir, generator, dataRoot, name)
	if err != nil {
		return projectconfig.Project{}, err
	}
	service, err := Open(ctx, project)
	if err != nil {
		return projectconfig.Project{}, err
	}
	return project, service.Close(ctx)
}

// Status describes a located project and, when its database exists, the
// opened Board.
type Status struct {
	ProjectRoot    string `json:"project_root"`
	ProjectName    string `json:"project_name"`
	ProjectID      string `json:"project_id"`
	DataDir        string `json:"data_dir"`
	DatabasePath   string `json:"database_path"`
	DatabaseExists bool   `json:"database_exists"`
	// SchemaVersion is the migrated schema version; zero when the database
	// does not exist yet.
	SchemaVersion int `json:"schema_version,omitempty"`
}

// Check reports where the project's Board lives. An existing database is
// opened, which applies pending migrations and verifies the project binding;
// a missing one is reported, not created.
func Check(ctx context.Context, project projectconfig.Project) (Status, error) {
	status := Status{
		ProjectRoot:  project.Root,
		ProjectName:  project.Identity.Name,
		ProjectID:    project.Identity.ProjectID,
		DataDir:      project.DataDir,
		DatabasePath: project.DatabasePath,
	}
	if _, err := os.Stat(project.DatabasePath); errors.Is(err, os.ErrNotExist) {
		return status, nil
	} else if err != nil {
		return status, domain.WrapError(err, domain.CodeStorageUnavailable, "cannot inspect database "+project.DatabasePath, false)
	}
	service, err := Open(ctx, project)
	if err != nil {
		return status, err
	}
	status.DatabaseExists = true
	status.SchemaVersion = migrations.CurrentVersion()
	return status, service.Close(ctx)
}
