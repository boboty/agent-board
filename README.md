# Agent Board

Agent Board is a harness-independent shared task board for AI-assisted software delivery.

It has two intentionally separate parts:

1. **Workflow Skill** — defines how Orchestrator, Developer, and Independent Verifier cooperate.
2. **Shared Task Board** — records tasks, explicit task-level state, execution facts, delivery evidence, handoffs, and audit history outside the repository/worktree.

## Core boundary

> **Code provides capabilities. The Skill defines the rules.**

The Board is a shared task ledger, not a workflow engine. It does not infer workflow semantics from leases, reviews, or agent runtime state.

Task-level state is explicit and limited to:

- `READY`
- `IN_PROGRESS`
- `DONE`
- `BLOCKED`

Infrastructure may provide persistence, concurrency, transport, and auditability, but it must not redefine product workflow semantics.

## Project status

This repository is the clean implementation of Agent Board. The earlier rhizome-based proof of concept is retained separately as `boboty/agent-board-rhizome-poc` for reference and selective infrastructure reuse.

See:

- `PRODUCT.md`
- `ARCHITECTURE.md`
- `workflow/SKILL.md`
- `AGENTS.md`

## Usage

```bash
go build -o aboard ./cmd/aboard
```

The Board database lives outside the repository, at
`~/.agent-board/<project_id>/board.db` on every platform. The committed
`.agent-board.json` carries the project ID, so every worktree and process of
the repository opens the same database. Run `aboard init` once for a new
repository and commit the identity file; `aboard check` shows what a directory
resolves to.

### MCP

`aboard mcp` discovers the project from its working directory and serves
the Board over stdio. `aboard mcp config` prints a generic stdio launch
configuration; pass `claude-code`, `codex`, or `opencode` to print a
Harness-specific snippet. Each generated configuration supplies a stable
`AGENT_BOARD_ACTOR` value: `harness/generic`, `harness/claude-code`,
`harness/codex`, or `harness/opencode`. For example:

```json
{
  "command": "/absolute/path/to/aboard",
  "args": ["mcp"],
  "env": {"AGENT_BOARD_ACTOR": "harness/generic"}
}
```

Harness snippets identify their target command or config file. Claude Code
prints a `claude mcp add` command, Codex prints a TOML section for
`~/.codex/config.toml`, and OpenCode prints a JSON fragment for
`~/.config/opencode/opencode.json`. Merge file fragments into existing
configuration as needed; `mcp config` only prints text and never edits Harness
files. It resolves the running executable path so the generated command works
even when the binary is not on the Harness's `PATH`. The actor value is an
audit label identifying the configuration's Harness source. It is not
authorization and does not claim a Human, Orchestrator, Developer, or Verifier
role. An explicit `actor` supplied with an MCP tool call takes precedence and
is recorded for that mutation; calls without it use the configured default.
This is independent of any role or session actor used by the Workflow Skill.
The same Harness always gets the same default label, while labels distinguish
the Harness source. No shell environment setup is required to use the printed
configuration.

Tools: `create_task`, `get_task`, `list_tasks`, `update_task`, `queue_task`,
`set_task_state`, `list_ready`, `reorder_ready`, `record_fact`, `list_facts`,
`list_events`. Failed calls return `isError` with
`{"error": {"code", "message", "details", "retryable"}}`.

### CLI

Run `aboard help` for the command list. CLI operation results are JSON on
stdout; errors are `{"error": {...}}` on stderr with exit status 1 (Board or
storage error) or 2 (usage error). `aboard call <operation> '<json>'`
invokes any operation through the same dispatch as MCP `tools/call`. The
default actor is `$AGENT_BOARD_ACTOR`, overridden by `--actor`.

`aboard doctor` prints human-readable status for the binary/version,
Workflow Skill, project identity, Board database file, and MCP operation
catalog. Exit status 0 means no problem was detected by its read-only checks,
1 means a component needs attention, and 2 means invalid usage. It does not
install the Skill, initialize projects, connect to SQLite, or run migrations.
For an existing Board file, it checks that the file is regular and has a
recognizable SQLite format; `Board PRESENT` means those file checks passed, not
that the database was opened or its contents validated. MCP status confirms
that the operation definitions are loaded; it does not start an MCP service.

### Workflow Skill

`workflow/SKILL.md` is the canonical Skill source and is embedded in the
`aboard` binary. A binary can install or display it without access to the
source repository:

```bash
aboard skill install
aboard skill check
aboard skill show
```

Install writes the same embedded content to Claude Code and OpenCode at
`~/.claude/skills/agent-board-workflow/SKILL.md`, and to Codex at
`~/.agents/skills/agent-board-workflow/SKILL.md`. Claude Code's personal path
is `~/.claude/skills`; Codex's documented user path is `~/.agents/skills`.
OpenCode scans both directories, and its current CLI resolves a same-name
skill from these compatibility sources once (the Claude-compatible location
wins). The old `~/.codex/skills` copy is also scanned by Codex when it is
under `CODEX_HOME`, so keeping it alongside `~/.agents/skills` caused Codex to
list the Skill twice. See the [Claude Code skills locations](https://code.claude.com/docs/en/skills),
[Codex local skill locations](https://learn.chatgpt.com/docs/build-skills), and
[OpenCode skill discovery](https://opencode.ai/docs/skills).

`skill check` returns JSON status and paths for each Harness, plus a legacy
Codex path when present. The old `~/.codex/skills/agent-board-workflow/SKILL.md`
path is no longer installed. `skill install` removes that file only when its
contents exactly match the embedded canonical Skill; a different file is kept
and reported with a manual next step. Install does not overwrite different
content at a supported path. Re-running install on matching files is
idempotent. `skill show` prints the embedded Markdown as plain text.

### Web Board

`aboard web` serves a browser Board for people on a loopback address
(default `127.0.0.1:7420`; `--addr 127.0.0.1:0` picks a free port) and prints
its URL as JSON. Four columns — READY, IN PROGRESS, DONE, BLOCKED — come
directly from each task's recorded `state`; unqueued tasks are listed
separately, not as a column. A task drawer shows content, facts, and audit
history, and offers edit, queue, set state, and record fact; READY cards move
up and down. Every write calls the same operations as MCP and the CLI, so the
Board's version checks, idempotency, transactions, and audit apply unchanged.
Writes are recorded with `--actor` / `$AGENT_BOARD_ACTOR`, else `web`. The
page follows changes made by any process (polling with ETags).

Only loopback binds are accepted. Requests must name the bound host
(`127.0.0.1:PORT` or `localhost:PORT`) and a same-origin `Origin`; writes also
need the per-process CSRF token embedded in the page. The page loads no
inline script or style (strict CSP).

Real-browser tests live in the separate `e2e` module (headless Chrome; set
`CHROME_PATH` if Chrome is not in the default location):

```bash
cd e2e && go test ./...
```

## License

Apache-2.0. Third-party code selectively reused from other projects must retain the required attribution and license notices.
