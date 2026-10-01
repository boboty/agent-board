// Package projectconfig discovers and initializes a repository's project
// identity and resolves the shared database path outside the repository, so
// every worktree and harness of one project opens the same Board database.
//
// Derived from rhizome-mcp (https://github.com/Odrin/rhizome-mcp), via
// boboty/agent-board-rhizome-poc, licensed under Apache-2.0. See NOTICE.
// Modified: renamed identity file; data lives under ~/.agent-board on every
// platform; removed rollback, root-only loading, and diagnostic helpers.
package projectconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/ids"
)

const (
	// IdentityFileName is the repository-local project identity file.
	IdentityFileName = ".agent-board.json"
	// CurrentIdentityVersion is the only identity format version supported.
	CurrentIdentityVersion = 1
	// DatabaseFileName is the Board database file inside a project data directory.
	DatabaseFileName = "board.db"
	// DataRootName is the directory below the user's home that holds every
	// project's Board data, on every platform.
	DataRootName = ".agent-board"

	// CodeProjectNotFound means no identity file exists at or above the start path.
	CodeProjectNotFound = "PROJECT_NOT_FOUND"
	// CodeInvalidIdentity means an identity file is unsafe or has invalid contents.
	CodeInvalidIdentity = "INVALID_PROJECT_IDENTITY"
	// CodeProjectAlreadyInitialized means the repository already has an identity entry.
	CodeProjectAlreadyInitialized = "PROJECT_ALREADY_INITIALIZED"
	// CodePathResolution means the data location cannot be resolved.
	CodePathResolution = "APP_DATA_PATH_ERROR"
	// CodeInitializationFailed means project initialization could not be completed.
	CodeInitializationFailed = "PROJECT_INITIALIZATION_FAILED"
	// CodeDiscoveryFailed means the start path or filesystem could not be inspected.
	CodeDiscoveryFailed = "PROJECT_DISCOVERY_FAILED"
)

// Identity is the complete on-disk .agent-board.json model.
type Identity struct {
	Version   int    `json:"version"`
	ProjectID string `json:"project_id"`
}

// Project identifies a repository and its external storage locations. DataDir
// and DatabasePath are empty for projects returned by Discover.
type Project struct {
	Root         string
	Identity     Identity
	DataDir      string
	DatabasePath string
}

// IDGenerator is the project-ID generator used by Initialize.
type IDGenerator interface {
	New() (string, error)
}

// Discover searches upward from start for a strict project identity. If start
// names a file, discovery begins in its containing directory.
func Discover(start string) (Project, error) {
	dir, err := discoveryStart(start)
	if err != nil {
		return Project{}, domain.WrapError(err, CodeDiscoveryFailed, "cannot inspect project discovery start", false)
	}
	for {
		identityPath := filepath.Join(dir, IdentityFileName)
		info, lstatErr := os.Lstat(identityPath)
		switch {
		case lstatErr == nil:
			if !info.Mode().IsRegular() {
				return Project{}, invalidIdentity(errors.New("identity path is not a regular file"))
			}
			identity, readErr := readIdentity(identityPath)
			if readErr != nil {
				return Project{}, readErr
			}
			return Project{Root: dir, Identity: identity}, nil
		case errors.Is(lstatErr, fs.ErrNotExist):
		default:
			return Project{}, domain.WrapError(lstatErr, CodeDiscoveryFailed, "cannot inspect project identity", false)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Project{}, domain.NewError(CodeProjectNotFound, "project identity not found", false)
		}
		dir = parent
	}
}

// ResolveDataRoot returns the Board data root, ~/.agent-board, from the
// supplied home directory. It does not read the environment or current user.
func ResolveDataRoot(homeDir string) (string, error) {
	if homeDir == "" {
		return "", pathError("home directory is required")
	}
	return filepath.Join(homeDir, DataRootName), nil
}

// ProjectDatabasePath returns <dataRoot>/<project_id>/board.db after
// validating that projectID is canonical.
func ProjectDatabasePath(dataRoot, projectID string) (string, error) {
	canonical, err := canonicalProjectID(projectID)
	if err != nil {
		return "", invalidIdentity(err)
	}
	if dataRoot == "" {
		return "", pathError("data root is required")
	}
	return filepath.Join(dataRoot, canonical, DatabaseFileName), nil
}

// Initialize creates a new repository identity and project data directory.
// Existing identity destinations are never overwritten. dataRoot must resolve
// outside the repository, and every precondition is validated before any
// write; a failure removes only directories created by this call.
func Initialize(repositoryRoot string, generator IDGenerator, dataRoot string) (Project, error) {
	root, err := canonicalDirectory(repositoryRoot)
	if err != nil {
		return Project{}, domain.WrapError(err, CodeInitializationFailed, "repository root must be an existing directory", false)
	}
	if generator == nil {
		return Project{}, domain.NewError(CodeInitializationFailed, "project ID generator is required", false)
	}
	if dataRoot == "" {
		return Project{}, pathError("data root is required")
	}
	identityPath := filepath.Join(root, IdentityFileName)
	if _, err := os.Lstat(identityPath); err == nil {
		return Project{}, domain.NewError(CodeProjectAlreadyInitialized, "project identity already exists at "+identityPath, false)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Project{}, domain.WrapError(err, CodeInitializationFailed, "cannot inspect identity destination", false)
	}
	if err := validateNewDataRootLocation(root, dataRoot); err != nil {
		return Project{}, domain.WrapError(err, domain.CodeStorageConfiguration, "data root must be outside the repository", false)
	}

	generated, err := generator.New()
	if err != nil {
		return Project{}, domain.WrapError(err, CodeInitializationFailed, "cannot generate project ID", false)
	}
	projectID, err := canonicalProjectID(generated)
	if err != nil {
		return Project{}, domain.WrapError(err, CodeInitializationFailed, "generated project ID is invalid", false)
	}
	identity := Identity{Version: CurrentIdentityVersion, ProjectID: projectID}
	dataDir := filepath.Join(dataRoot, projectID)

	createdDirs, err := createDirectories(dataDir)
	if err != nil {
		cleanupDirectories(createdDirs)
		return Project{}, domain.WrapError(err, CodeInitializationFailed, "cannot create project data directory", false)
	}
	if err := createIdentityAtomically(root, identity); err != nil {
		cleanupDirectories(createdDirs)
		if errors.Is(err, fs.ErrExist) {
			return Project{}, domain.WrapError(err, CodeProjectAlreadyInitialized, "project identity already exists at "+identityPath, false)
		}
		return Project{}, domain.WrapError(err, CodeInitializationFailed, "cannot create project identity", false)
	}
	return Project{
		Root:         root,
		Identity:     identity,
		DataDir:      dataDir,
		DatabasePath: filepath.Join(dataDir, DatabaseFileName),
	}, nil
}

// validateNewDataRootLocation confirms that dataRoot, which may not exist yet,
// resolves outside repositoryRoot. Only its nearest existing ancestor is
// symlink-resolved; missing trailing components are rejoined.
func validateNewDataRootLocation(repositoryRoot, dataRoot string) error {
	absolute, err := filepath.Abs(dataRoot)
	if err != nil {
		return err
	}
	cursor := filepath.Clean(absolute)
	var missing []string
	for {
		info, err := os.Stat(cursor)
		if err == nil {
			if !info.IsDir() {
				return errors.New("data root is not a directory")
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(cursor)
		if parent == cursor {
			return errors.New("data root has no existing ancestor")
		}
		missing = append(missing, filepath.Base(cursor))
		cursor = parent
	}
	resolved, err := filepath.EvalSymlinks(cursor)
	if err != nil {
		return err
	}
	for i := len(missing) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, missing[i])
	}
	relative, err := filepath.Rel(repositoryRoot, resolved)
	if err != nil {
		return err
	}
	if relative == "." || (relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return errors.New("data root is inside the repository")
	}
	return nil
}

func discoveryStart(start string) (string, error) {
	if start == "" {
		return "", errors.New("start path is required")
	}
	absolute, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if info.Mode().IsRegular() {
		return filepath.Dir(resolved), nil
	}
	if !info.IsDir() {
		return "", errors.New("start path is neither a directory nor a regular file")
	}
	return resolved, nil
}

func readIdentity(path string) (Identity, error) {
	file, err := os.Open(path)
	if err != nil {
		return Identity{}, invalidIdentity(err)
	}
	defer file.Close()
	identity, err := decodeIdentity(file)
	if err != nil {
		return Identity{}, invalidIdentity(err)
	}
	return identity, nil
}

func decodeIdentity(reader io.Reader) (Identity, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var identity Identity
	if err := decoder.Decode(&identity); err != nil {
		return Identity{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Identity{}, errors.New("trailing data after identity object")
	}
	if identity.Version != CurrentIdentityVersion {
		return Identity{}, fmt.Errorf("unsupported identity version %d", identity.Version)
	}
	canonical, err := canonicalProjectID(identity.ProjectID)
	if err != nil {
		return Identity{}, err
	}
	identity.ProjectID = canonical
	return identity, nil
}

func canonicalProjectID(value string) (string, error) {
	if value == "" {
		return "", errors.New("project_id is required")
	}
	parsed, err := ids.ParseStrict(value)
	if err != nil {
		return "", fmt.Errorf("invalid project_id: %w", err)
	}
	return parsed.String(), nil
}

func canonicalDirectory(root string) (string, error) {
	if root == "" {
		return "", errors.New("repository root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("repository root is not a directory")
	}
	return resolved, nil
}

// createIdentityAtomically writes the identity to a temporary file and
// installs it with a hard link, which (unlike rename) never replaces an
// existing destination.
func createIdentityAtomically(root string, identity Identity) error {
	contents, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')

	temporary, err := os.CreateTemp(root, IdentityFileName+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Link(temporaryPath, filepath.Join(root, IdentityFileName))
}

func createDirectories(target string) ([]string, error) {
	target = filepath.Clean(target)
	var missing []string
	for cursor := target; ; {
		info, err := os.Stat(cursor)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("%s is not a directory", cursor)
			}
			if cursor == target {
				return nil, errors.New("project data path already exists")
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		missing = append(missing, cursor)
		parent := filepath.Dir(cursor)
		if parent == cursor {
			break
		}
		cursor = parent
	}
	created := make([]string, 0, len(missing))
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o700); err != nil {
			if errors.Is(err, fs.ErrExist) && missing[i] != target {
				if info, statErr := os.Stat(missing[i]); statErr == nil && info.IsDir() {
					continue
				}
			}
			return created, err
		}
		created = append(created, missing[i])
	}
	return created, nil
}

func cleanupDirectories(created []string) {
	for i := len(created) - 1; i >= 0; i-- {
		_ = os.Remove(created[i])
	}
}

func invalidIdentity(cause error) error {
	return domain.WrapError(cause, CodeInvalidIdentity, "project identity is invalid", false)
}

func pathError(message string) error {
	return domain.NewError(CodePathResolution, message, false)
}
