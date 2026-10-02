# Agent Board

[中文](README.md) | [English](README_EN.md)

**A local-first, harness-independent shared task board for AI-assisted software delivery.**

AI agents are good at producing todo lists, but long-lived state is a different problem. Once work spans sessions, agents, harnesses, or worktrees, state kept in chat history, a todo list, or one agent runtime quickly becomes stale.

Agent Board extracts **task-level shared state** into a stable ledger so task management and task execution can evolve independently.

> **Code provides capabilities. Skills define the rules.**

```text
Human / Codex / Claude Code / OpenCode
                 ↓
      agent-board-management
                 ↓
            Agent Board
                 ↓
       agent-board-workflow
                 ↓
      Orchestrator / Harness
          ↓             ↓
     Developer   Independent Verifier
```

The management side and execution side may use the same harness or completely different ones. One harness is enough; multiple harnesses and parallel Orchestrators are optional enhancements.

## Why Agent Board

- **State does not live in chat**: Tasks, READY order, delivery, verification, handoff, and audit history live in a shared ledger.
- **Management is decoupled from execution**: discuss and queue work in one conversation, then let another Orchestrator consume READY tasks.
- **Harness-independent**: CLI is the baseline; Web and MCP are adapters over the same Board operations.
- **Local-first**: no account, cloud service, or database server is required; the Board uses local SQLite.
- **Parallel-friendly**: independent Tasks can run in separate worktrees while sharing one Board.
- **Validation stays proportional to risk**: Developer and Independent Verifier use the smallest sufficient evidence instead of defaulting small changes to repository-wide checks.

## 30-second quick start

Requirement: **Go 1.25+**.

```bash
go install github.com/boboty/agent-board/cmd/aboard@latest
aboard skill install
```

Make sure the Go bin directory is on `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Initialize a project:

```bash
cd your-project
aboard init
aboard doctor
aboard web
```

`aboard init` creates `.agent-board.json`. Commit it to the repository so every worktree and process of the project resolves to the same Board.

`aboard web` listens on a free loopback port by default and prints the actual URL, so multiple project Boards can run at the same time.

## Core model

A Task has exactly four explicit states:

- `READY`
- `IN_PROGRESS`
- `DONE`
- `BLOCKED`

A Task may exist as an unqueued draft. Queue membership is orthogonal metadata, not a fifth lifecycle state.

The Board records facts such as:

- Task definition and version
- READY ordering
- explicit Task state
- execution / delivery / verification / handoff facts
- immutable audit events

The Board does **not** decide who develops, when a Verifier starts, how RC works, or when DONE is semantically allowed. Those rules belong to Skills.

## Two independent Skills

### `agent-board-management`

Used for task management: create, edit, accept, queue, reorder, and inspect Tasks. It does not start implementation.

A typical use is to discuss an idea in Codex, Claude Code, or another local harness, let the agent turn the mature idea into a Board Task, and keep it unqueued until a Human accepts readiness.

### `agent-board-workflow`

Used for execution. It defines the boundaries between Orchestrator, Developer, and Independent Verifier.

Core rules include:

- Developer is the only implementation writer and owns implementation plus self-check;
- Independent Verifier runs as a fresh, separate, read-only session;
- after PASS, Orchestrator packages the verified workspace into the accepted commit;
- the accepted commit must exactly match the PASS baseline / fingerprint;
- validation is derived from the Task acceptance boundary and plausible impact radius instead of defaulting to unrelated repository-wide checks.

The two Skills are independent and can be installed, checked, or shown separately:

```bash
aboard skill install
aboard skill check
aboard skill show management
aboard skill show workflow
```

Skills are installed into user-level harness discovery paths: Claude Code / OpenCode use `~/.claude/skills`, while Codex uses `~/.agents/skills`. `aboard skill install` also safely handles managed legacy copies left by earlier Agent Board versions.

## A typical workflow

```text
1. Human discusses work in the preferred harness
2. Management Skill turns a mature idea into an unqueued Task
3. Human accepts it and queues it as READY
4. Orchestrator consumes READY and creates an isolated worktree
5. Developer implements and self-checks
6. a fresh Independent Verifier performs read-only verification
7. after PASS, Orchestrator packages the accepted commit
8. with Human authorization, merge / push / cleanup may continue
```

If two READY Tasks are independent and have sufficiently separate change surfaces, different Orchestrators can process them in parallel. The Board does not need extra lifecycle states for parallelism; Git and the Orchestrator own integration.

## CLI, Web, and MCP

### CLI

`aboard` is the baseline interface. Common commands:

```bash
aboard board
aboard task list
aboard ready list
aboard history 12
aboard doctor
aboard operations
```

CLI operation results are JSON. `aboard call <operation> '<json>'` invokes the same dispatch used by MCP.

### Web Board

`aboard web` provides a local human-facing Board with:

- READY / IN PROGRESS / DONE / BLOCKED columns;
- unqueued Tasks shown separately;
- Task details, facts, and audit history;
- READY reordering;
- a bounded recent DONE list ordered by the latest transition into DONE;
- a paginated `/completed/` history.

Only loopback binds are accepted.

### MCP (optional)

MCP is not required to use Agent Board. When structured tool access is useful, a harness can connect to the same Board operations over stdio:

```bash
aboard mcp config claude-code
aboard mcp config codex
aboard mcp config opencode
```

`mcp config` only prints configuration; it does not edit harness config files.

## Local storage

Board data lives at:

```text
~/.agent-board/<project_id>/board.db
```

The repository-level `.agent-board.json` stores only the project identity. Multiple worktrees, CLI processes, Web servers, MCP clients, and agents for the same project therefore share one Board without committing the SQLite database into the repository.

## What Agent Board intentionally does not do

Agent Board keeps a deliberately narrow boundary. It is not a:

- workflow engine
- Agent runtime
- model router
- cloud collaboration platform
- replacement for an IDE or harness

It focuses on one composable piece:

> **a stable, shared, auditable task ledger between task management and task execution.**

Other projects can add remote access, cloud sync, team services, desktop apps, or other surrounding capabilities without turning the core Board into a platform.

## Project docs

- [PRODUCT.md](PRODUCT.md) — product definition and boundaries
- [ARCHITECTURE.md](ARCHITECTURE.md) — architecture and persistence direction
- [management/SKILL.md](management/SKILL.md) — Board Management Skill
- [workflow/SKILL.md](workflow/SKILL.md) — Workflow Skill
- [AGENTS.md](AGENTS.md) — repository agent instructions

## Development from source

Normal users do not need to clone the repository. For development:

```bash
git clone https://github.com/boboty/agent-board.git
cd agent-board
go build -o aboard ./cmd/aboard
```

Real-browser e2e tests live in a separate module:

```bash
cd e2e && go test ./...
```

## License

Apache-2.0. Third-party code selectively reused from other projects must retain the required attribution and license notices.
