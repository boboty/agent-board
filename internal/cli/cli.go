// Package cli is a thin, scriptable command-line adapter over the Board
// operations. Commands build ops arguments from flags, call ops.Service, and
// print the result as JSON on stdout. Failures print {"error": {...}} on
// stderr with exit status 1 (Board or storage error) or 2 (usage error).
package cli

import (
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
  check                        show project discovery and database location
  doctor                       diagnose installation and current project setup
  board                        show the READY queue and every task
  mcp                          serve the Board as MCP tools over stdio
  mcp config [HARNESS]         print stdio MCP configuration (generic, claude-code, codex, opencode)
  web [--addr 127.0.0.1:7420]  serve the Web Board on a loopback address

Skills:
  skill install [management|workflow] 安装一个或全部内嵌 Skill
  skill check [management|workflow]   检查一个或全部 Skill 安装状态
  skill show management|workflow      显示指定的内嵌 Skill

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
	if len(args) > 1 {
		return usagef("skill install accepts at most one selector: management or workflow")
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
		targets := uniqueSkillTargets(skillTargets(env.Home, skill.Name))
		statuses := make([]skillInstallStatus, 0, len(targets)+1)
		for _, target := range targets {
			status, err := installSkill(target, skill.Content)
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

func installSkill(target skillTarget, content string) (string, error) {
	contents, err := os.ReadFile(target.Path)
	switch {
	case err == nil && string(contents) == content:
		return "current", nil
	case err == nil:
		return "different", nil
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
		lines = append(lines, doctorLine{"Binary", "PROBLEM", "无法确定可执行文件路径: " + err.Error(), "检查当前 binary 的安装和运行权限。"})
		healthy = false
	} else {
		lines = append(lines, doctorLine{"Binary", "OK", fmt.Sprintf("%s (version %s)", executable, version), ""})
	}

	var skillDetails []string
	skillStatusError := ""
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
			}
			if item.Harness == "claude-code" || item.Harness == "codex" || item.Harness == "opencode" {
				states = append(states, item.Harness+"="+item.Status)
			}
			if item.Status == "managed-legacy" || item.Status == "unmanaged-legacy" {
				legacy = item.Status + ": " + item.Path
			}
		}
		label := "Workflow"
		if skill.Name == "management" {
			label = "Management"
		}
		purpose := "指导 Task 执行、交付和验证"
		if skill.Name == "management" {
			purpose = "指导 Task 定义和 READY 优先级（使用 CLI，MCP 可选）"
		}
		state := fmt.Sprintf("%s Skill — %s; %d/3 Harness (%s)", label, purpose, current, strings.Join(states, ", "))
		if different > 0 {
			state += fmt.Sprintf("; %d 处内容与 binary 不同", different)
		}
		if current < 3 {
			state += "; 有 Harness 尚未安装"
		}
		if legacy != "" {
			state += "; 旧路径 " + legacy
		}
		skillDetails = append(skillDetails, state)
	}
	if skillStatusError != "" {
		lines = append(lines, doctorLine{"Skills", "PROBLEM", "无法读取 Skill 安装状态: " + skillStatusError, "检查当前用户对 Skill 目录的读取权限。"})
		healthy = false
	} else {
		status, next := "OK", ""
		for _, detail := range skillDetails {
			if strings.Contains(detail, "内容与 binary 不同") || strings.Contains(detail, "旧路径") {
				status = "PROBLEM"
			}
			if strings.Contains(detail, "有 Harness 尚未安装") && status == "OK" {
				status = "MISSING"
			}
		}
		if status != "OK" {
			healthy = false
			next = "运行 `aboard skill check management` 或 `aboard skill check workflow` 检查对应内容；安装时可用 `aboard skill install management|workflow`。"
			for _, detail := range skillDetails {
				if strings.Contains(detail, "旧路径 unmanaged-legacy:") {
					next += " 检查旧路径是否为用户自定义内容；确认后手动移走或合并，aboard 不会覆盖或删除它。"
				} else if strings.Contains(detail, "旧路径 managed-legacy:") {
					next += " Workflow 旧副本与 canonical 内容一致，可运行 `aboard skill install workflow` 移除。"
				}
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
		lines = append(lines, doctorLine{"Project", "MISSING", "未找到 .agent-board.json（当前目录及其父目录均未初始化）", "在项目根目录运行 `aboard init`。"})
		healthy = false
	default:
		lines = append(lines, doctorLine{"Project", "PROBLEM", projectErr.Error(), "检查 .agent-board.json 内容、文件类型和读取权限。"})
		healthy = false
	}

	if projectErr == nil {
		if locateErr != nil {
			lines = append(lines, doctorLine{"Board", "PROBLEM", "无法解析数据库路径: " + locateErr.Error(), "检查 HOME 和 Agent Board 数据目录配置。"})
			healthy = false
		} else if info, statErr := os.Stat(located.DatabasePath); errors.Is(statErr, os.ErrNotExist) {
			lines = append(lines, doctorLine{"Board", "MISSING", located.DatabasePath, "数据库尚未创建；运行 `aboard board` 会按正常应用行为初始化它。"})
			healthy = false
		} else if statErr != nil {
			lines = append(lines, doctorLine{"Board", "PROBLEM", located.DatabasePath + ": " + statErr.Error(), "检查数据库路径及其父目录的访问权限。"})
			healthy = false
		} else if !info.Mode().IsRegular() {
			lines = append(lines, doctorLine{"Board", "PROBLEM", located.DatabasePath + " 不是普通文件", "检查数据库路径；将它恢复为有效的 board.db 文件。"})
			healthy = false
		} else {
			validHeader, headerErr := hasSQLiteHeader(located.DatabasePath)
			switch {
			case headerErr != nil:
				lines = append(lines, doctorLine{"Board", "PROBLEM", located.DatabasePath + ": " + headerErr.Error(), "检查数据库文件的读取权限；doctor 未打开或修复数据库。"})
				healthy = false
			case !validHeader:
				lines = append(lines, doctorLine{"Board", "PROBLEM", located.DatabasePath + " 不是有效的 SQLite 文件头", "检查文件是否被覆盖或损坏，并从可信备份恢复正确的项目数据库。"})
				healthy = false
			default:
				lines = append(lines, doctorLine{"Board", "PRESENT", located.DatabasePath + " (数据库文件存在，SQLite 格式可识别)", ""})
			}
		}
	} else {
		if domain.IsCode(projectErr, projectconfig.CodeProjectNotFound) {
			lines = append(lines, doctorLine{"Board", "MISSING", "没有项目 identity，因此无法解析 Board 数据库路径", "先在项目根目录运行 `aboard init`。"})
		} else {
			lines = append(lines, doctorLine{"Board", "PROBLEM", "项目 identity 不可用，无法解析 Board 数据库路径", "先修复 Project 项报告的 .agent-board.json 问题。"})
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
		lines = append(lines, doctorLine{"MCP", "OK", fmt.Sprintf("%d 个操作及输入定义已加载（未启动 MCP 服务）", len(operations)), ""})
	} else {
		lines = append(lines, doctorLine{"MCP", "PROBLEM", "operation registry or input schema is unavailable", "重新构建或重新安装 aboard binary。"})
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
	addr := c.flags.String("addr", "127.0.0.1:7420", "loopback address to listen on (port 0 picks a free port)")
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
