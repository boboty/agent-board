// Package cli is a thin, scriptable command-line adapter over the Board
// operations. Commands build ops arguments from flags, call ops.Service, and
// print the result as JSON on stdout. Failures print {"error": {...}} on
// stderr with exit status 1 (Board or storage error) or 2 (usage error).
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/boboty/agent-board/internal/board"
	"github.com/boboty/agent-board/internal/domain"
	"github.com/boboty/agent-board/internal/mcpserver"
	"github.com/boboty/agent-board/internal/ops"
	"github.com/boboty/agent-board/internal/projectconfig"
	"github.com/boboty/agent-board/internal/workspace"
)

// Env is everything the CLI reads from its process.
type Env struct {
	// Dir is the default project discovery start (the working directory).
	Dir string
	// Paths selects the application-data root.
	Paths projectconfig.PathInputs
	// Actor is the default actor label (AGENT_BOARD_ACTOR).
	Actor   string
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

// Exit statuses.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// CodeUsage identifies a malformed command line.
const CodeUsage = "USAGE"

const usage = `usage: agent-board <command> [flags]

Project:
  init                         create .agent-board.json here and the shared Board database
  check                        show project discovery and database location
  board                        show the READY queue and every task
  mcp                          serve the Board as MCP tools over stdio

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
		writeError(env.Stderr, domain.NewError(CodeUsage, usageErr.message+" (run agent-board help)", false))
		return ExitUsage
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
	case "board":
		return runBoard(ctx, rest, env)
	case "mcp":
		return runMCP(ctx, rest, env)
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
				fmt.Fprintf(c.env.Stdout, "usage: agent-board %s\n", c.name)
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
	project, err := workspace.Locate(c.dir, c.env.Paths)
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
	project, err := workspace.Init(ctx, c.dir, env.Paths)
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
	project, err := workspace.Locate(c.dir, env.Paths)
	if err != nil {
		return err
	}
	status, err := workspace.Check(ctx, project)
	if err != nil {
		return err
	}
	return writeJSON(env.Stdout, status)
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
