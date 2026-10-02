// Package cli is a thin, scriptable command-line adapter over the Board
// operations. Commands build ops arguments from flags, call ops.Service, and
// print the result as JSON on stdout. Failures print {"error": {...}} on
// stderr with exit status 1 (Board or storage error) or 2 (usage error).
package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/boboty/agent-board/internal/board"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/mcpserver"
	"github.com/boboty/agent-board/internal/ops"
	"github.com/boboty/agent-board/internal/projectconfig"
	"github.com/boboty/agent-board/internal/web"
	"github.com/boboty/agent-board/internal/workspace"
	"github.com/boboty/agent-board/management"
	"github.com/boboty/agent-board/workflow"
)

// Env is everything the CLI reads from its process.
type Env struct {
	// Dir is the default project discovery start (the working directory).
	Dir string
	// Home is the user's home directory; the Board data root is Home/.agent-board.
	Home string
	// Actor is the default actor label (AGENT_BOARD_ACTOR).
	Actor   string
	Version string
	// ExecutablePath resolves the currently running executable. Tests may inject it.
	ExecutablePath func() (string, error)
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
}

// Exit statuses.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// CodeUsage identifies a malformed command line.
const CodeUsage = "USAGE"

const usage = `usage: aboard <command> [flags]

Project:
  init                         create .agent-board.json here and the shared Board database
  clean [--yes]                remove this project's identity and shared Board data
  uninstall [--yes] [--force]  remove aboard, its Skills, and all local Board data
  check                        show project discovery and database location
  doctor                       diagnose installation and current project setup
  board                        show the READY queue and every task
  mcp                          serve the Board as MCP tools over stdio
  mcp config [HARNESS]         print stdio MCP configuration (generic, claude-code, codex, opencode)
  web [--addr HOST:PORT]       serve the Web Board on a loopback address (default: free port on 127.0.0.1)

Skills:
  skill install [--force] [management|workflow] install all embedded Skills by default, or select one
  skill check [management|workflow]   check installation status for all Skills by default, or select one
  skill show management|workflow      display the specified embedded Skill

Tasks:
  task create --title T [--description D] [--acceptance A]
  task get REF
  task list [--state S]... [--queued | --unqueued]
  task update REF --version N [--title T] [--description D] [--acceptance A]
  task queue REF --version N
  task set-state REF STATE --version N [--reason R]

READY:
  ready list
  ready reorder --version N REF...

Facts and history:
  fact record REF --kind K (--body B | --body-file PATH|-) [--data JSON]
  fact list REF [--kind K]
  history REF                  facts and audit events of one task
  events [--task REF] [--after ID] [--limit N]

Raw operation (same dispatch as MCP tools/call):
  call OPERATION [ARGS_JSON | -]
  operations                   list operations with their input schemas

Common flags:
  --dir PATH      project discovery start (default: working directory)
  --actor LABEL   actor recorded for mutations (default: $AGENT_BOARD_ACTOR)
  --key KEY       idempotency key for a mutation

REF is a task ID or number (12 or #12). Output is JSON.
doctor prints human-readable component status; exit 0 means no problems were found by its read-only checks, 1 means a component needs attention, and 2 means invalid usage. It checks that the Board file exists and has a recognizable SQLite format; it does not connect to the database. MCP status reports that operation definitions are loaded; it does not start an MCP service.
`

type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

func usagef(format string, args ...any) error {
	return usageError{fmt.Sprintf(format, args...)}
}

// Run executes one command line and returns the exit status.
func Run(ctx context.Context, args []string, env Env) int {
	err := run(ctx, args, env)
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, flag.ErrHelp):
		return ExitOK
	}
	var usageErr usageError
	if errors.As(err, &usageErr) {
		writeError(env.Stderr, domain.NewError(CodeUsage, usageErr.message+" (run aboard help)", false))
		return ExitUsage
	}
	var healthErr doctorHealthError
	if errors.As(err, &healthErr) {
		return ExitError
	}
	var uninstallErr uninstallIncomplete
	if errors.As(err, &uninstallErr) {
		return ExitError
	}
	writeError(env.Stderr, ops.DescribeError(err))
	return ExitError
}

func writeError(w io.Writer, err *domain.Error) {
	encoded, _ := json.Marshal(map[string]any{"error": err})
	fmt.Fprintln(w, string(encoded))
}

func writeJSON(w io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(encoded))
	return err
}

func run(ctx context.Context, args []string, env Env) error {
	if len(args) == 0 {
		return usagef("missing command")
	}
	name, rest := args[0], args[1:]
	switch name {
	case "help", "-h", "--help":
		fmt.Fprint(env.Stdout, usage)
		return nil
	case "version", "--version":
		fmt.Fprintln(env.Stdout, env.Version)
		return nil
	case "init":
		return runInit(ctx, rest, env)
	case "clean":
		return runClean(rest, env)
	case "uninstall":
		return runUninstall(rest, env)
	case "check":
		return runCheck(ctx, rest, env)
	case "doctor":
		return runDoctor(ctx, rest, env)
	case "board":
		return runBoard(ctx, rest, env)
	case "mcp":
		if len(rest) > 0 && rest[0] == "config" {
			return runMCPConfig(rest[1:], env)
		}
		return runMCP(ctx, rest, env)
	case "web":
		return runWeb(ctx, rest, env)
	case "skill":
		if len(rest) == 0 {
			return usagef("skill: missing subcommand")
		}
		switch rest[0] {
		case "install":
			return runSkillInstall(rest[1:], env)
		case "check":
			return runSkillCheck(rest[1:], env)
		case "show":
			return runSkillShow(rest[1:], env)
		default:
			return usagef("unknown command %q", "skill "+rest[0])
		}
	case "events":
		return runEvents(ctx, rest, env)
	case "history":
		return runHistory(ctx, rest, env)
	case "call":
		return runCall(ctx, rest, env)
	case "operations":
		return runOperations(rest, env)
	case "task", "ready", "fact":
		if len(rest) == 0 {
			return usagef("%s: missing subcommand", name)
		}
		sub, subArgs := rest[0], rest[1:]
		handler, ok := subcommands[name+" "+sub]
		if !ok {
			return usagef("unknown command %q", name+" "+sub)
		}
		return handler(ctx, subArgs, env)
	default:
		return usagef("unknown command %q", name)
	}
}

func runClean(args []string, env Env) error {
	c := newCommand("clean", env)
	yes := c.flags.Bool("yes", false, "confirm removal without prompting")
	if _, err := c.parse(args, 0, 0); err != nil {
		return err
	}
	project, err := workspace.Locate(c.dir, env.Home)
	if err != nil {
		return err
	}
	identityPath := filepath.Join(project.Root, projectconfig.IdentityFileName)
	fmt.Fprintf(env.Stdout, "Project identity: %s\nBoard data: %s\n", identityPath, project.DataDir)
	if !*yes {
		fmt.Fprint(env.Stdout, "Remove this project's Agent Board data and identity? [y/N] ")
		line, readErr := bufio.NewReader(env.Stdin).ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return domain.NewError("PROJECT_CLEAN_FAILED", "cannot read confirmation; nothing was removed", false)
		}
		answer := strings.TrimSpace(line)
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Fprintln(env.Stdout, "Cleanup cancelled; nothing was removed.")
			return nil
		}
	}
	if err := os.RemoveAll(project.DataDir); err != nil {
		return domain.WrapError(err, "PROJECT_CLEAN_FAILED", "cannot remove Board data directory "+project.DataDir+"; project identity was kept", false)
	}
	if err := os.Remove(identityPath); err != nil {
		return domain.WrapError(err, "PROJECT_CLEAN_FAILED", "Board data was removed, but cannot remove project identity "+identityPath, false)
	}
	fmt.Fprintln(env.Stdout, "Project Agent Board data and identity removed.")
	return nil
}

// uninstallIncomplete marks an uninstall that reported its per-path failures
// itself, so Run can return a failure status without obscuring that report.
type uninstallIncomplete struct{}

func (uninstallIncomplete) Error() string { return "Agent Board uninstall was incomplete" }

func runUninstall(args []string, env Env) error {
	yes, force := false, false
	for _, arg := range args {
		switch arg {
		case "--yes":
			if yes {
				return usagef("uninstall accepts --yes at most once")
			}
			yes = true
		case "--force":
			if force {
				return usagef("uninstall accepts --force at most once")
			}
			force = true
		default:
			return usagef("unknown uninstall argument %q (choose --yes or --force)", arg)
		}
	}
	resolve := env.ExecutablePath
	if resolve == nil {
		resolve = runningExecutable
	}
	executable, err := resolve()
	if err != nil {
		return fmt.Errorf("resolve aboard executable: %w", err)
	}
	if !filepath.IsAbs(executable) {
		return fmt.Errorf("resolve aboard executable: path is not absolute: %q", executable)
	}
	if env.Home == "" || !filepath.IsAbs(env.Home) {
		return fmt.Errorf("resolve Agent Board data root: HOME is empty or not absolute")
	}
	dataRoot := filepath.Join(env.Home, ".agent-board")
	definitions := skillDefinitions()
	targets := make([]skillTarget, 0, 7)
	for _, skill := range definitions {
		targets = append(targets, uniqueSkillTargets(skillTargets(env.Home, skill.Name))...)
	}
	targets = append(targets, legacySkillTarget(env.Home))
	fmt.Fprintf(env.Stdout, "Uninstall targets:\n  Binary: %s\n", executable)
	for _, target := range targets {
		fmt.Fprintf(env.Stdout, "  %s Skill: %s\n", target.Harness, target.Path)
	}
	fmt.Fprintf(env.Stdout, "  Local data: %s\n", dataRoot)
	if !yes {
		fmt.Fprint(env.Stdout, "Uninstall Agent Board from this machine? [y/N] ")
		line, readErr := bufio.NewReader(env.Stdin).ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			fmt.Fprintln(env.Stdout, "Uninstall cancelled; nothing was removed.")
			return nil
		}
		answer := strings.TrimSpace(line)
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			fmt.Fprintln(env.Stdout, "Uninstall cancelled; nothing was removed.")
			return nil
		}
	}
	failed := false
	var preservedItems, failedItems []string
	for _, target := range targets {
		status, inspectErr := inspectUninstallSkill(target, definitions)
		if inspectErr != nil {
			fmt.Fprintf(env.Stdout, "FAILED Skill %s: %v\n", target.Path, inspectErr)
			failedItems = append(failedItems, target.Path)
			failed = true
			continue
		}
		switch status {
		case "missing":
			fmt.Fprintf(env.Stdout, "MISSING Skill: %s\n", target.Path)
		case "unmanaged":
			fmt.Fprintf(env.Stdout, "PRESERVED unmanaged Skill (left in place; remove manually if no longer needed): %s\n", target.Path)
			preservedItems = append(preservedItems, target.Path)
			failed = true
		case "modified":
			if !force {
				fmt.Fprintf(env.Stdout, "PRESERVED modified Agent Board Skill (left in place; remove manually, or reinstall aboard before using --force): %s\n", target.Path)
				preservedItems = append(preservedItems, target.Path)
				failed = true
				continue
			}
		}
		if status == "current" || status == "modified" && force {
			if removeErr := os.Remove(target.Path); removeErr != nil {
				fmt.Fprintf(env.Stdout, "FAILED Skill %s: %v\n", target.Path, removeErr)
				failedItems = append(failedItems, target.Path)
				failed = true
				continue
			}
			fmt.Fprintf(env.Stdout, "REMOVED Skill: %s\n", target.Path)
			var stop string
			if target.Harness == "codex-legacy" {
				stop = filepath.Join(env.Home, ".codex", "skills")
			} else if target.Harness == "codex" {
				stop = filepath.Join(env.Home, ".agents", "skills")
			} else {
				stop = filepath.Join(env.Home, ".claude", "skills")
			}
			removeEmptySkillDirs(filepath.Dir(target.Path), stop)
		}
	}
	dataRemoved, err := removeDataRoot(dataRoot)
	if err != nil {
		fmt.Fprintf(env.Stdout, "FAILED local data %s: %v\n", dataRoot, err)
		fmt.Fprintf(env.Stdout, "Local data remains or may be partially removed; inspect and remove it manually: %s\n", dataRoot)
		failedItems = append(failedItems, dataRoot)
		failed = true
	} else if dataRemoved {
		fmt.Fprintf(env.Stdout, "REMOVED local data: %s\n", dataRoot)
	} else {
		fmt.Fprintf(env.Stdout, "MISSING local data: %s\n", dataRoot)
	}
	binaryStatus := "REMOVED"
	if err := os.Remove(executable); err != nil {
		binaryStatus = "FAILED"
		fmt.Fprintf(env.Stdout, "FAILED binary %s: %v\n", executable, err)
		failedItems = append(failedItems, executable)
		failed = true
	} else {
		fmt.Fprintf(env.Stdout, "REMOVED binary: %s\n", executable)
	}
	if failed {
		fmt.Fprintf(env.Stdout, "Agent Board uninstall incomplete.\nBinary: %s (%s)\n", binaryStatus, executable)
		if len(preservedItems) > 0 {
			fmt.Fprintln(env.Stdout, "Preserved items requiring manual handling:")
			for _, path := range preservedItems {
				fmt.Fprintf(env.Stdout, "  %s\n", path)
			}
		}
		if len(failedItems) > 0 {
			fmt.Fprintln(env.Stdout, "Failed items requiring inspection and manual cleanup:")
			for _, path := range failedItems {
				fmt.Fprintf(env.Stdout, "  %s\n", path)
			}
		}
		return uninstallIncomplete{}
	}
	fmt.Fprintln(env.Stdout, "Agent Board uninstall complete.")
	return nil
}

func removeDataRoot(root string) (bool, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("expected a real directory; preserving non-directory or symlink")
	}
	if err := os.RemoveAll(root); err != nil {
		return false, err
	}
	return true, nil
}

func inspectUninstallSkill(target skillTarget, definitions []skillDefinition) (string, error) {
	info, err := os.Lstat(target.Path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || uninstallSkillPathHasSymlink(target) {
		return "unmanaged", nil
	}
	contents, err := os.ReadFile(target.Path)
	if err != nil {
		return "", err
	}
	if target.Harness == "codex-legacy" {
		if string(contents) == workflow.Skill {
			return "current", nil
		}
		return "unmanaged", nil
	}
	for _, skill := range definitions {
		if filepath.Base(filepath.Dir(target.Path)) == "agent-board-"+skill.Name {
			if string(contents) == skill.Content {
				return "current", nil
			}
			return "modified", nil
		}
	}
	return "unmanaged", nil
}

func uninstallSkillPathHasSymlink(target skillTarget) bool {
	path := target.Path
	const stopDepth = 4 // file, skill directory, skills directory, harness directory, HOME
	for depth := 0; depth <= stopDepth; depth++ {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return true
		}
		if depth == stopDepth {
			return false
		}
		path = filepath.Dir(path)
	}
	return false
}

type skillTarget struct {
	Harness string `json:"harness"`
	Path    string `json:"path"`
}

type skillDefinition struct {
	Name    string
	Content string
}

func skillDefinitions() []skillDefinition {
	return []skillDefinition{{Name: "management", Content: management.Skill}, {Name: "workflow", Content: workflow.Skill}}
}

func selectSkills(selector string, required bool) ([]skillDefinition, error) {
	if selector == "" {
		if required {
			return nil, usagef("skill show requires management or workflow (usage: aboard skill show management|workflow)")
		}
		return skillDefinitions(), nil
	}
	for _, skill := range skillDefinitions() {
		if skill.Name == selector {
			return []skillDefinition{skill}, nil
		}
	}
	return nil, usagef("unknown Skill %q (choose management or workflow)", selector)
}

func skillTargets(home, skillName string) []skillTarget {
	claude := filepath.Join(home, ".claude", "skills", "agent-board-"+skillName, "SKILL.md")
	return []skillTarget{
		{"claude-code", claude},
		{"codex", filepath.Join(home, ".agents", "skills", "agent-board-"+skillName, "SKILL.md")},
		{"opencode", claude},
	}
}

func legacySkillTarget(home string) skillTarget {
	return skillTarget{"codex-legacy", filepath.Join(home, ".codex", "skills", "agent-board-workflow", "SKILL.md")}
}

type skillInstallStatus struct {
	skillTarget
	Status string `json:"status"`
}

func runSkillInstall(args []string, env Env) error {
	selector := ""
	force := false
	for _, arg := range args {
		switch arg {
		case "--force":
			if force {
				return usagef("skill install accepts --force at most once")
			}
			force = true
		case "management", "workflow":
			if selector != "" {
				return usagef("skill install accepts at most one selector: management or workflow")
			}
			selector = arg
		default:
			return usagef("unknown skill install argument %q (choose --force, management, or workflow)", arg)
		}
	}
	selected, err := selectSkills(selector, false)
	if err != nil {
		return err
	}
	allStatuses := make([]map[string]any, 0, len(selected))
	for _, skill := range selected {
		targets := uniqueSkillTargets(skillTargets(env.Home, skill.Name))
		statuses := make([]skillInstallStatus, 0, len(targets)+1)
		for _, target := range targets {
			status, err := installSkill(target, skill.Content, force)
			if err != nil {
				return err
			}
			statuses = append(statuses, skillInstallStatus{skillTarget: target, Status: status})
		}
		if skill.Name == "workflow" {
			legacy, err := inspectLegacySkill(env.Home)
			if err != nil {
				return err
			}
			if legacy.Status == "managed-legacy" {
				if err := os.Remove(legacy.Path); err != nil {
					return fmt.Errorf("remove legacy Codex skill %s: %w", legacy.Path, err)
				}
				removeEmptySkillDirs(filepath.Dir(legacy.Path), filepath.Join(env.Home, ".codex", "skills"))
				legacy.Status = "clear"
			} else if legacy.Status == "missing" {
				legacy.Status = "clear"
			}
			statuses = append(statuses, legacy)
		}
		allStatuses = append(allStatuses, map[string]any{"skill": skill.Name, "status": statuses})
	}
	return writeJSON(env.Stdout, map[string]any{"skills": allStatuses})
}

func uniqueSkillTargets(targets []skillTarget) []skillTarget {
	seen := make(map[string]bool, len(targets))
	unique := make([]skillTarget, 0, len(targets))
	for _, target := range targets {
		if !seen[target.Path] {
			seen[target.Path] = true
			unique = append(unique, target)
		}
	}
	return unique
}

func installSkill(target skillTarget, content string, force bool) (string, error) {
	contents, err := os.ReadFile(target.Path)
	switch {
	case err == nil && string(contents) == content:
		return "current", nil
	case err == nil && !force:
		return "different", nil
	case err == nil:
		if err := replaceSkillFile(target.Path, content); err != nil {
			return "", fmt.Errorf("upgrade %s skill at %s: %w", target.Harness, target.Path, err)
		}
		return "current", nil
	case !errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("check %s skill at %s: %w", target.Harness, target.Path, err)
	}
	if err := os.MkdirAll(filepath.Dir(target.Path), 0o755); err != nil {
		return "", fmt.Errorf("create %s skill directory: %w", target.Harness, err)
	}
	if err := os.WriteFile(target.Path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("install %s skill at %s: %w", target.Harness, target.Path, err)
	}
	return "current", nil
}

func replaceSkillFile(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".SKILL.md-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := io.WriteString(tmp, content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func removeEmptySkillDirs(dir, stop string) {
	for dir != stop && strings.HasPrefix(dir, stop+string(os.PathSeparator)) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func runSkillCheck(args []string, env Env) error {
	if len(args) > 1 {
		return usagef("skill check accepts at most one selector: management or workflow")
	}
	selector := ""
	if len(args) == 1 {
		selector = args[0]
	}
	selected, err := selectSkills(selector, false)
	if err != nil {
		return err
	}
	allStatuses := make([]map[string]any, 0, len(selected))
	for _, skill := range selected {
		statuses, err := checkSkillStatus(env.Home, skill)
		if err != nil {
			return err
		}
		allStatuses = append(allStatuses, map[string]any{"skill": skill.Name, "status": statuses})
	}
	return writeJSON(env.Stdout, map[string]any{"skills": allStatuses})
}

func checkSkillStatus(home string, skill skillDefinition) ([]skillInstallStatus, error) {
	targets := skillTargets(home, skill.Name)
	statuses := make([]skillInstallStatus, 0, len(targets))
	for _, target := range targets {
		contents, err := os.ReadFile(target.Path)
		status := "missing"
		switch {
		case err == nil && string(contents) == skill.Content:
			status = "current"
		case err == nil:
			status = "different"
		case !errors.Is(err, os.ErrNotExist):
			return nil, fmt.Errorf("check %s skill: %w", target.Harness, err)
		}
		statuses = append(statuses, skillInstallStatus{skillTarget: target, Status: status})
	}
	if skill.Name == "workflow" {
		legacy, err := inspectLegacySkill(home)
		if err != nil {
			return nil, err
		}
		if legacy.Status != "missing" {
			statuses = append(statuses, legacy)
		}
	}
	return statuses, nil
}

func inspectLegacySkill(home string) (skillInstallStatus, error) {
	target := legacySkillTarget(home)
	info, err := os.Lstat(target.Path)
	if errors.Is(err, os.ErrNotExist) {
		return skillInstallStatus{skillTarget: target, Status: "missing"}, nil
	}
	if err != nil {
		return skillInstallStatus{}, fmt.Errorf("check legacy Codex skill at %s: %w", target.Path, err)
	}
	if !info.Mode().IsRegular() || legacySkillPathHasSymlink(home) {
		return skillInstallStatus{skillTarget: target, Status: "unmanaged-legacy"}, nil
	}
	contents, err := os.ReadFile(target.Path)
	switch {
	case err == nil && string(contents) == workflow.Skill:
		return skillInstallStatus{skillTarget: target, Status: "managed-legacy"}, nil
	case err == nil:
		return skillInstallStatus{skillTarget: target, Status: "unmanaged-legacy"}, nil
	case errors.Is(err, os.ErrNotExist):
		return skillInstallStatus{skillTarget: target, Status: "missing"}, nil
	default:
		return skillInstallStatus{}, fmt.Errorf("check legacy Codex skill at %s: %w", target.Path, err)
	}
}

func legacySkillPathHasSymlink(home string) bool {
	path := home
	for _, part := range []string{".codex", "skills", "agent-board-workflow"} {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

func runSkillShow(args []string, env Env) error {
	if len(args) != 1 {
		return usagef("skill show requires management or workflow (usage: aboard skill show management|workflow)")
	}
	selected, err := selectSkills(args[0], true)
	if err != nil {
		return err
	}
	_, err = io.WriteString(env.Stdout, selected[0].Content)
	return err
}

var subcommands map[string]func(context.Context, []string, Env) error

func init() {
	subcommands = map[string]func(context.Context, []string, Env) error{
		"task create":    runTaskCreate,
		"task get":       runTaskGet,
		"task list":      runTaskList,
		"task update":    runTaskUpdate,
		"task queue":     runTaskQueue,
		"task set-state": runTaskSetState,
		"ready list":     runReadyList,
		"ready reorder":  runReadyReorder,
		"fact record":    runFactRecord,
		"fact list":      runFactList,
	}
}

// command is one parsed command line with the common flags.
type command struct {
	name  string
	env   Env
	flags *flag.FlagSet
	dir   string
	actor string
	key   string
}

func newCommand(name string, env Env) *command {
	c := &command{name: name, env: env, flags: flag.NewFlagSet(name, flag.ContinueOnError)}
	c.flags.SetOutput(io.Discard)
	c.flags.StringVar(&c.dir, "dir", env.Dir, "project discovery start")
	c.flags.StringVar(&c.actor, "actor", env.Actor, "actor recorded for mutations")
	return c
}

// withKey adds --key to a mutating command.
func (c *command) withKey() *command {
	c.flags.StringVar(&c.key, "key", "", "idempotency key")
	return c
}

// parse accepts flags before, between, and after positional arguments and
// checks the positional count.
func (c *command) parse(args []string, minArgs, maxArgs int) ([]string, error) {
	var positional []string
	for {
		if err := c.flags.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fmt.Fprintf(c.env.Stdout, "usage: aboard %s\n", c.name)
				c.flags.SetOutput(c.env.Stdout)
				c.flags.PrintDefaults()
				return nil, err
			}
			return nil, usagef("%s: %v", c.name, err)
		}
		rest := c.flags.Args()
		if len(rest) == 0 {
			break
		}
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			positional = append(positional, rest...)
			break
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
	if len(positional) < minArgs || (maxArgs >= 0 && len(positional) > maxArgs) {
		return nil, usagef("%s: wrong number of arguments", c.name)
	}
	return positional, nil
}

func (c *command) set(name string) bool {
	found := false
	c.flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func (c *command) write() ops.Write {
	return ops.Write{Actor: c.actor, IdempotencyKey: c.key}
}

// open locates the project from --dir and opens its Board.
func (c *command) open(ctx context.Context) (*ops.Service, projectconfig.Project, func(), error) {
	project, err := workspace.Locate(c.dir, c.env.Home)
	if err != nil {
		return nil, projectconfig.Project{}, nil, err
	}
	service, err := workspace.Open(ctx, project)
	if err != nil {
		return nil, projectconfig.Project{}, nil, err
	}
	return ops.New(service, c.actor), project, func() { _ = service.Close(context.WithoutCancel(ctx)) }, nil
}

// do opens the Board, runs fn, and prints its result.
func do[R any](ctx context.Context, c *command, fn func(*ops.Service) (R, error)) error {
	service, _, closeBoard, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer closeBoard()
	result, err := fn(service)
	if err != nil {
		return err
	}
	return writeJSON(c.env.Stdout, result)
}

func versionFlag(c *command) *int64 {
	return c.flags.Int64("version", 0, "expected version")
}

func requireFlag(c *command, name string) error {
	if !c.set(name) {
		return usagef("%s: --%s is required", c.name, name)
	}
	return nil
}

func runInit(ctx context.Context, args []string, env Env) error {
	c := newCommand("init", env)
	if _, err := c.parse(args, 0, 0); err != nil {
		return err
	}
	project, err := workspace.Init(ctx, c.dir, env.Home)
	if err != nil {
		return err
	}
	status, err := workspace.Check(ctx, project)
	if err != nil {
		return err
	}
	return writeJSON(env.Stdout, status)
}

func runCheck(ctx context.Context, args []string, env Env) error {
	c := newCommand("check", env)
	if _, err := c.parse(args, 0, 0); err != nil {
		return err
	}
	project, err := workspace.Locate(c.dir, env.Home)
	if err != nil {
		return err
	}
	status, err := workspace.Check(ctx, project)
	if err != nil {
		return err
	}
	return writeJSON(env.Stdout, status)
}

type doctorHealthError struct{}

func (doctorHealthError) Error() string { return "doctor found components that need attention" }

type doctorLine struct {
	name, status, detail, next string
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func runDoctor(ctx context.Context, args []string, env Env) error {
	c := newCommand("doctor", env)
	if _, err := c.parse(args, 0, 0); err != nil {
		return err
	}
	lines := make([]doctorLine, 0, 5)
	healthy := true

	resolve := env.ExecutablePath
	if resolve == nil {
		resolve = runningExecutable
	}
	executable, err := resolve()
	version := env.Version
	if version == "" {
		version = "unknown"
	}
	if err != nil {
		lines = append(lines, doctorLine{"Binary", "PROBLEM", "Could not determine the executable path: " + err.Error(), "Check the binary installation and execution permissions."})
		healthy = false
	} else {
		lines = append(lines, doctorLine{"Binary", "OK", fmt.Sprintf("%s (version %s)", executable, version), ""})
	}

	var skillDetails []string
	skillStatusError := ""
	skillHasDifferent, skillHasMissing := false, false
	skillHasManagedLegacy, skillHasUnmanagedLegacy := false, false
	for _, skill := range skillDefinitions() {
		skillStatuses, statusErr := checkSkillStatus(env.Home, skill)
		if statusErr != nil {
			skillStatusError = statusErr.Error()
			break
		}
		current, different := 0, 0
		var states []string
		legacy := ""
		for _, item := range skillStatuses {
			if item.Status == "current" {
				current++
			}
			if item.Status == "different" {
				different++
				skillHasDifferent = true
			}
			if item.Status == "missing" {
				skillHasMissing = true
			}
			if item.Harness == "claude-code" || item.Harness == "codex" || item.Harness == "opencode" {
				states = append(states, item.Harness+"="+item.Status)
			}
			if item.Status == "managed-legacy" || item.Status == "unmanaged-legacy" {
				legacy = item.Status + ": " + item.Path
				skillHasManagedLegacy = skillHasManagedLegacy || item.Status == "managed-legacy"
				skillHasUnmanagedLegacy = skillHasUnmanagedLegacy || item.Status == "unmanaged-legacy"
			}
		}
		label := "Workflow"
		if skill.Name == "management" {
			label = "Management"
		}
		purpose := "guides Task execution, delivery, and verification"
		if skill.Name == "management" {
			purpose = "guides Task definition and READY prioritization (CLI; MCP optional)"
		}
		missing := len(states) - current - different
		state := fmt.Sprintf("%s Skill — %s; %d of %d harnesses are current (%s)", label, purpose, current, len(states), strings.Join(states, ", "))
		if different > 0 {
			state += fmt.Sprintf("; %d installed %s %s different content", different, plural(different, "copy", "copies"), plural(different, "has", "have"))
		}
		if missing > 0 {
			state += fmt.Sprintf("; %d %s missing", missing, plural(missing, "harness is", "harnesses are"))
		}
		if legacy != "" {
			state += "; legacy path " + legacy
		}
		skillDetails = append(skillDetails, state)
	}
	if skillStatusError != "" {
		lines = append(lines, doctorLine{"Skills", "PROBLEM", "Could not read Skill installation status: " + skillStatusError, "Check read access to the current user's Skill directories."})
		healthy = false
	} else {
		status, next := "OK", ""
		if skillHasDifferent || skillHasManagedLegacy || skillHasUnmanagedLegacy {
			status = "PROBLEM"
		} else if skillHasMissing {
			status = "MISSING"
		}
		if status != "OK" {
			healthy = false
			next = "Run `aboard skill check management` or `aboard skill check workflow` to inspect the corresponding content."
			if skillHasDifferent {
				next += " Different installed Skill content is preserved by default. To overwrite the selected Skill with this binary's embedded content, run `aboard skill install --force management` or `aboard skill install --force workflow`."
			}
			if skillHasMissing {
				next += " Install missing Skills with `aboard skill install management` or `aboard skill install workflow`."
			}
			if skillHasUnmanagedLegacy {
				next += " Check whether the legacy path contains user-owned content; after review, move or merge it manually. aboard will not overwrite or delete it."
			}
			if skillHasManagedLegacy {
				next += " The legacy Workflow copy matches the canonical content; run `aboard skill install workflow` to remove it."
			}
		}
		lines = append(lines, doctorLine{"Skills", status, strings.Join(skillDetails, "; "), next})
	}

	project, projectErr := projectconfig.Discover(c.dir)
	located, locateErr := workspace.Locate(c.dir, env.Home)
	switch {
	case projectErr == nil:
		lines = append(lines, doctorLine{"Project", "OK", fmt.Sprintf("%s (project_id %s)", project.Root, project.Identity.ProjectID), ""})
	case domain.IsCode(projectErr, projectconfig.CodeProjectNotFound):
		lines = append(lines, doctorLine{"Project", "MISSING", "Could not find .agent-board.json in this directory or any parent directory.", "Run `aboard init` from the project root."})
		healthy = false
	default:
		lines = append(lines, doctorLine{"Project", "PROBLEM", projectErr.Error(), "Check the contents, file type, and read permissions of .agent-board.json."})
		healthy = false
	}

	if projectErr == nil {
		if locateErr != nil {
			lines = append(lines, doctorLine{"Board", "PROBLEM", "Could not resolve the database path: " + locateErr.Error(), "Check HOME and the Agent Board data directory configuration."})
			healthy = false
		} else if info, statErr := os.Stat(located.DatabasePath); errors.Is(statErr, os.ErrNotExist) {
			lines = append(lines, doctorLine{"Board", "MISSING", located.DatabasePath, "The database has not been created; running `aboard board` will initialize it as part of normal operation."})
			healthy = false
		} else if statErr != nil {
			lines = append(lines, doctorLine{"Board", "PROBLEM", located.DatabasePath + ": " + statErr.Error(), "Check access to the database path and its parent directory."})
			healthy = false
		} else if !info.Mode().IsRegular() {
			lines = append(lines, doctorLine{"Board", "PROBLEM", located.DatabasePath + " is not a regular file", "Check the database path and restore it as a valid board.db file."})
			healthy = false
		} else {
			validHeader, headerErr := hasSQLiteHeader(located.DatabasePath)
			switch {
			case headerErr != nil:
				lines = append(lines, doctorLine{"Board", "PROBLEM", located.DatabasePath + ": " + headerErr.Error(), "Check read access to the database file. doctor does not open or repair the database."})
				healthy = false
			case !validHeader:
				lines = append(lines, doctorLine{"Board", "PROBLEM", located.DatabasePath + " does not have a valid SQLite file header", "Check whether the file was replaced or corrupted, and restore the project database from a trusted backup."})
				healthy = false
			default:
				lines = append(lines, doctorLine{"Board", "PRESENT", located.DatabasePath + " (database file exists and has a recognizable SQLite format)", ""})
			}
		}
	} else {
		if domain.IsCode(projectErr, projectconfig.CodeProjectNotFound) {
			lines = append(lines, doctorLine{"Board", "MISSING", "No project identity is available, so the Board database path cannot be resolved.", "Run `aboard init` from the project root first."})
		} else {
			lines = append(lines, doctorLine{"Board", "PROBLEM", "The project identity is unavailable, so the Board database path cannot be resolved.", "Fix the .agent-board.json issue reported under Project first."})
		}
		healthy = false
	}

	operations := ops.Operations()
	loaded := len(operations) > 0
	for _, operation := range operations {
		if operation.Name == "" || operation.InputSchema == nil {
			loaded = false
			break
		}
	}
	// Construct the same MCP server used by the `mcp` command; it only registers
	// the catalog and does not open a project or database.
	if loaded {
		_ = mcpserver.New(nil, mcpserver.Info{Version: env.Version})
		lines = append(lines, doctorLine{"MCP", "OK", fmt.Sprintf("%d operation definitions and input schemas loaded (MCP service not started)", len(operations)), ""})
	} else {
		lines = append(lines, doctorLine{"MCP", "PROBLEM", "The operation registry or input schema is unavailable.", "Rebuild or reinstall the aboard binary."})
		healthy = false
	}

	for _, line := range lines {
		fmt.Fprintf(env.Stdout, "%-8s %-7s %s\n", line.name, line.status, line.detail)
		if line.next != "" {
			fmt.Fprintf(env.Stdout, "         Next step: %s\n", line.next)
		}
	}
	if !healthy {
		return doctorHealthError{}
	}
	return nil
}

func hasSQLiteHeader(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	header := make([]byte, 16)
	if _, err := io.ReadFull(file, header); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, nil
		}
		return false, err
	}
	return bytes.Equal(header, []byte("SQLite format 3\x00")), nil
}

func runBoard(ctx context.Context, args []string, env Env) error {
	c := newCommand("board", env)
	if _, err := c.parse(args, 0, 0); err != nil {
		return err
	}
	service, project, closeBoard, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer closeBoard()
	ready, err := service.ListReady(ctx, ops.ListReadyArgs{})
	if err != nil {
		return err
	}
	tasks, err := service.ListTasks(ctx, ops.ListTasksArgs{})
	if err != nil {
		return err
	}
	return writeJSON(env.Stdout, map[string]any{
		"project_id": project.Identity.ProjectID,
		"ready":      ready,
		"tasks":      tasks.Tasks,
	})
}

func runMCP(ctx context.Context, args []string, env Env) error {
	c := newCommand("mcp", env)
	if _, err := c.parse(args, 0, 0); err != nil {
		return err
	}
	service, project, closeBoard, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer closeBoard()
	server := mcpserver.New(service, mcpserver.Info{
		Version:      env.Version,
		ProjectID:    project.Identity.ProjectID,
		DatabasePath: project.DatabasePath,
	})
	stdin := &eofReader{reader: env.Stdin}
	transport := &mcp.IOTransport{Reader: io.NopCloser(stdin), Writer: nopWriteCloser{env.Stdout}}
	err = server.Run(ctx, transport)
	// The client closing stdin, or a signal, is a normal stdio shutdown.
	if stdin.eof.Load() || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// DefaultWebActor is recorded for Web Board mutations when neither --actor
// nor $AGENT_BOARD_ACTOR names one.
const DefaultWebActor = "web"

func runWeb(ctx context.Context, args []string, env Env) error {
	c := newCommand("web", env)
	addr := c.flags.String("addr", "127.0.0.1:0", "loopback address to listen on (default: free port chosen by the OS; the URL is printed on start)")
	if _, err := c.parse(args, 0, 0); err != nil {
		return err
	}
	if _, err := web.ValidateLoopbackAddress(*addr); err != nil {
		return usagef("web: %v", err)
	}
	if strings.TrimSpace(c.actor) == "" {
		c.actor = DefaultWebActor
	}
	service, project, closeBoard, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer closeBoard()
	handler, err := web.NewHandler(service, web.Info{ProjectID: project.Identity.ProjectID, Actor: c.actor})
	if err != nil {
		return err
	}
	return web.Serve(ctx, web.ServerOptions{
		Address: *addr,
		Handler: handler,
		OnListen: func(listener net.Listener) {
			_ = writeJSON(env.Stdout, map[string]string{
				"url":           "http://" + listener.Addr().String() + "/",
				"project_id":    project.Identity.ProjectID,
				"database_path": project.DatabasePath,
				"actor":         c.actor,
			})
		},
	})
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// eofReader records that the client closed its end of the stdio stream.
type eofReader struct {
	reader io.Reader
	eof    atomic.Bool
}

func (r *eofReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if errors.Is(err, io.EOF) {
		r.eof.Store(true)
	}
	return n, err
}

func runTaskCreate(ctx context.Context, args []string, env Env) error {
	c := newCommand("task create", env).withKey()
	title := c.flags.String("title", "", "title")
	description := c.flags.String("description", "", "description")
	acceptance := c.flags.String("acceptance", "", "acceptance criteria")
	if _, err := c.parse(args, 0, 0); err != nil {
		return err
	}
	return do(ctx, c, func(s *ops.Service) (ops.TaskResult, error) {
		return s.CreateTask(ctx, ops.CreateTaskArgs{Write: c.write(), Title: *title, Description: *description, AcceptanceCriteria: *acceptance})
	})
}

func runTaskGet(ctx context.Context, args []string, env Env) error {
	c := newCommand("task get REF", env)
	positional, err := c.parse(args, 1, 1)
	if err != nil {
		return err
	}
	return do(ctx, c, func(s *ops.Service) (ops.TaskResult, error) {
		return s.GetTask(ctx, ops.GetTaskArgs{Task: positional[0]})
	})
}

func runTaskList(ctx context.Context, args []string, env Env) error {
	c := newCommand("task list", env)
	var states stringList
	c.flags.Var(&states, "state", "recorded state (repeatable or comma-separated)")
	queued := c.flags.Bool("queued", false, "only queued tasks")
	unqueued := c.flags.Bool("unqueued", false, "only unqueued tasks")
	if _, err := c.parse(args, 0, 0); err != nil {
		return err
	}
	in := ops.ListTasksArgs{}
	for _, state := range states {
		in.States = append(in.States, domain.State(state))
	}
	switch {
	case *queued && *unqueued:
		return usagef("task list: --queued and --unqueued are exclusive")
	case *queued:
		in.Queued = queued
	case *unqueued:
		value := false
		in.Queued = &value
	}
	return do(ctx, c, func(s *ops.Service) (ops.TasksResult, error) { return s.ListTasks(ctx, in) })
}

func runTaskUpdate(ctx context.Context, args []string, env Env) error {
	c := newCommand("task update REF", env).withKey()
	version := versionFlag(c)
	title := c.flags.String("title", "", "new title")
	description := c.flags.String("description", "", "new description")
	acceptance := c.flags.String("acceptance", "", "new acceptance criteria")
	positional, err := c.parse(args, 1, 1)
	if err != nil {
		return err
	}
	if err := requireFlag(c, "version"); err != nil {
		return err
	}
	in := ops.UpdateTaskArgs{Write: c.write(), Task: positional[0], ExpectedVersion: *version}
	if c.set("title") {
		in.Title = title
	}
	if c.set("description") {
		in.Description = description
	}
	if c.set("acceptance") {
		in.AcceptanceCriteria = acceptance
	}
	return do(ctx, c, func(s *ops.Service) (ops.TaskResult, error) { return s.UpdateTask(ctx, in) })
}

func runTaskQueue(ctx context.Context, args []string, env Env) error {
	c := newCommand("task queue REF", env).withKey()
	version := versionFlag(c)
	positional, err := c.parse(args, 1, 1)
	if err != nil {
		return err
	}
	if err := requireFlag(c, "version"); err != nil {
		return err
	}
	return do(ctx, c, func(s *ops.Service) (ops.TaskResult, error) {
		return s.QueueTask(ctx, ops.QueueTaskArgs{Write: c.write(), Task: positional[0], ExpectedVersion: *version})
	})
}

func runTaskSetState(ctx context.Context, args []string, env Env) error {
	c := newCommand("task set-state REF STATE", env).withKey()
	version := versionFlag(c)
	reason := c.flags.String("reason", "", "state reason")
	positional, err := c.parse(args, 2, 2)
	if err != nil {
		return err
	}
	if err := requireFlag(c, "version"); err != nil {
		return err
	}
	in := ops.SetTaskStateArgs{Write: c.write(), Task: positional[0], ExpectedVersion: *version, State: domain.State(positional[1])}
	if c.set("reason") {
		in.Reason = reason
	}
	return do(ctx, c, func(s *ops.Service) (ops.TaskResult, error) { return s.SetTaskState(ctx, in) })
}

func runReadyList(ctx context.Context, args []string, env Env) error {
	c := newCommand("ready list", env)
	if _, err := c.parse(args, 0, 0); err != nil {
		return err
	}
	return do(ctx, c, func(s *ops.Service) (domain.ReadyQueue, error) { return s.ListReady(ctx, ops.ListReadyArgs{}) })
}

func runReadyReorder(ctx context.Context, args []string, env Env) error {
	c := newCommand("ready reorder REF...", env).withKey()
	version := versionFlag(c)
	positional, err := c.parse(args, 0, -1)
	if err != nil {
		return err
	}
	if err := requireFlag(c, "version"); err != nil {
		return err
	}
	return do(ctx, c, func(s *ops.Service) (domain.ReadyQueue, error) {
		return s.ReorderReady(ctx, ops.ReorderReadyArgs{Write: c.write(), ExpectedVersion: *version, Tasks: append([]string{}, positional...)})
	})
}

func runFactRecord(ctx context.Context, args []string, env Env) error {
	c := newCommand("fact record REF", env).withKey()
	kind := c.flags.String("kind", "", "fact kind")
	body := c.flags.String("body", "", "fact body")
	bodyFile := c.flags.String("body-file", "", "read the fact body from a file (- for stdin)")
	data := c.flags.String("data", "", "JSON object stored with the fact")
	positional, err := c.parse(args, 1, 1)
	if err != nil {
		return err
	}
	if c.set("body") && c.set("body-file") {
		return usagef("fact record: --body and --body-file are exclusive")
	}
	if c.set("body-file") {
		contents, err := readInput(*bodyFile, env.Stdin)
		if err != nil {
			return err
		}
		*body = string(contents)
	}
	in := ops.RecordFactArgs{Write: c.write(), Task: positional[0], Kind: domain.FactKind(*kind), Body: *body}
	if c.set("data") {
		in.Data = json.RawMessage(*data)
	}
	return do(ctx, c, func(s *ops.Service) (ops.FactResult, error) { return s.RecordFact(ctx, in) })
}

func runFactList(ctx context.Context, args []string, env Env) error {
	c := newCommand("fact list REF", env)
	kind := c.flags.String("kind", "", "fact kind")
	positional, err := c.parse(args, 1, 1)
	if err != nil {
		return err
	}
	return do(ctx, c, func(s *ops.Service) (ops.FactsResult, error) {
		return s.ListFacts(ctx, ops.ListFactsArgs{Task: positional[0], Kind: domain.FactKind(*kind)})
	})
}

func runHistory(ctx context.Context, args []string, env Env) error {
	c := newCommand("history REF", env)
	positional, err := c.parse(args, 1, 1)
	if err != nil {
		return err
	}
	service, _, closeBoard, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer closeBoard()
	task, err := service.GetTask(ctx, ops.GetTaskArgs{Task: positional[0]})
	if err != nil {
		return err
	}
	facts, err := service.ListFacts(ctx, ops.ListFactsArgs{Task: task.Task.ID})
	if err != nil {
		return err
	}
	events := []domain.TaskEvent{}
	for afterID := int64(0); ; {
		page, err := service.ListEvents(ctx, ops.ListEventsArgs{Task: task.Task.ID, AfterID: afterID, Limit: board.MaxEventLimit})
		if err != nil {
			return err
		}
		events = append(events, page.Events...)
		if len(page.Events) < board.MaxEventLimit {
			break
		}
		afterID = page.Events[len(page.Events)-1].ID
	}
	return writeJSON(env.Stdout, map[string]any{"task": task.Task, "facts": facts.Facts, "events": events})
}

func runEvents(ctx context.Context, args []string, env Env) error {
	c := newCommand("events", env)
	task := c.flags.String("task", "", "only this task's events")
	after := c.flags.Int64("after", 0, "exclusive event id cursor")
	limit := c.flags.Int("limit", 0, "page size")
	if _, err := c.parse(args, 0, 0); err != nil {
		return err
	}
	return do(ctx, c, func(s *ops.Service) (ops.EventsResult, error) {
		return s.ListEvents(ctx, ops.ListEventsArgs{Task: *task, AfterID: *after, Limit: *limit})
	})
}

func runCall(ctx context.Context, args []string, env Env) error {
	c := newCommand("call OPERATION [ARGS_JSON | -]", env)
	positional, err := c.parse(args, 1, 2)
	if err != nil {
		return err
	}
	arguments := json.RawMessage("{}")
	if len(positional) == 2 {
		arguments = json.RawMessage(positional[1])
		if positional[1] == "-" {
			contents, err := io.ReadAll(env.Stdin)
			if err != nil {
				return err
			}
			arguments = contents
		}
	}
	return do(ctx, c, func(s *ops.Service) (any, error) { return s.Call(ctx, positional[0], arguments) })
}

func runOperations(args []string, env Env) error {
	c := newCommand("operations", env)
	if _, err := c.parse(args, 0, 0); err != nil {
		return err
	}
	type entry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		ReadOnly    bool   `json:"read_only"`
		InputSchema any    `json:"input_schema"`
	}
	var entries []entry
	for _, op := range ops.Operations() {
		entries = append(entries, entry{op.Name, op.Description, op.ReadOnly, op.InputSchema})
	}
	return writeJSON(env.Stdout, entries)
}

// readInput reads stdin for "-" and the named file otherwise.
func readInput(name string, stdin io.Reader) ([]byte, error) {
	if name == "-" {
		return io.ReadAll(stdin)
	}
	contents, err := os.ReadFile(name)
	if err != nil {
		return nil, usagef("cannot read %s: %v", name, err)
	}
	return contents, nil
}

type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*l = append(*l, part)
		}
	}
	return nil
}
