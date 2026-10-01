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
go build -o agent-board ./cmd/agent-board
```

The Board database lives outside the repository, under the platform data
directory (`~/Library/Application Support/agent-board` on macOS,
`$XDG_DATA_HOME/agent-board` or `~/.local/share/agent-board` on Linux,
`%LOCALAPPDATA%\agent-board` on Windows), at
`projects/<project_id>/board.db`. The committed `.agent-board.json` carries
the project ID, so every worktree and process of the repository opens the same
database. Run `agent-board init` once for a new repository and commit the
identity file; `agent-board check` shows what a directory resolves to.

### MCP

`agent-board mcp` discovers the project from its working directory and serves
the Board over stdio. Example harness entry:

```json
{"command": "agent-board", "args": ["mcp", "--actor", "claude-code"]}
```

Tools: `create_task`, `get_task`, `list_tasks`, `update_task`, `queue_task`,
`set_task_state`, `list_ready`, `reorder_ready`, `record_fact`, `list_facts`,
`list_events`. Failed calls return `isError` with
`{"error": {"code", "message", "details", "retryable"}}`.

### CLI

Run `agent-board help` for the command list. Output is JSON on stdout; errors
are `{"error": {...}}` on stderr with exit status 1 (Board or storage error)
or 2 (usage error). `agent-board call <operation> '<json>'` invokes any
operation through the same dispatch as MCP `tools/call`. The default actor is
`$AGENT_BOARD_ACTOR`, overridden by `--actor`.

### Web Board

`agent-board web` serves a browser Board for people on a loopback address
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
