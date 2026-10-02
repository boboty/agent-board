# Agent Board

[中文](README.md) | [English](README_EN.md)

**Take AI development tasks out of chat history.**

If you have started giving Codex, Claude Code, OpenCode, or other agents a stream of development work, you have probably seen the same failure mode:

- the todo list looks good, then drifts away from reality;
- a new session needs the whole “where are we?” story again;
- a Developer says it is done, and you still have to chase verification;
- two agents run in parallel and nobody has a reliable shared view;
- the expensive part stops being coding and becomes supervision.

Agent Board does one simple thing: **put Tasks, state, delivery, and verification into a shared Board.**

You keep discussing ideas in the AI tool you already use. Once an idea is clear, turn it into a Task. When it enters READY, the execution layer can implement, verify, and close it. You come back when a decision is actually needed.

> The goal is not another board to maintain. The goal is fewer agents to babysit.

## See how it feels

Suppose you notice this in a project:

> The Skills section of `aboard --help` mixes Chinese and English.

You do not need to hand-write a task card or switch into a special execution UI.

### 1. Say one thing in Codex

> Turn this idea into a Board Task. Do not queue it yet and do not start implementation.

Codex can turn it into an unqueued Task. Review it in the Web Board, and when it looks right, say:

> I accept this Task. Queue it at the end of READY.

You can also queue it manually in the Web Board.

### 2. Let Paseo do the work

Start Paseo in the same project and tell it:

> Read and execute READY Tasks from the current project's Agent Board, following `agent-board-workflow`. Use the configured Paseo Agent Profiles. Continue until the READY queue is empty or a Human decision is required.

Then stop watching it.

Paseo reads READY, assigns a Developer, and starts a fresh Independent Verifier after delivery. After PASS, the Orchestrator packages the verified workspace into the accepted commit.

If you also want normal Git cleanup to happen automatically, add:

> When a Task is complete and verified, merge it into main, push origin/main, and clean up the corresponding local worktree and branch after the push succeeds. Stop and report on conflicts or unexpected conditions.

### 3. You see the result

```text
Idea
 ↓
Task
 ↓
READY
 ↓
Implementation + independent verification
 ↓
DONE
```

Most of the time, your job becomes deciding **what to do, what comes first, and what requires judgment**.

Which agent writes the code, which agent verifies it, and which worktree it runs in can stay in the execution layer.

## Start in 5 minutes

Requirement: **Go 1.25+**.

Install:

```bash
go install github.com/boboty/agent-board/cmd/aboard@latest
aboard skill install
```

If `aboard` is not on PATH:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Initialize your project:

```bash
cd your-project
aboard init
aboard doctor
aboard web
```

`aboard init` creates `.agent-board.json`. Commit it so every worktree and process for the same project resolves to the same Board.

`aboard web` automatically chooses a free `127.0.0.1` port and prints the URL. Multiple projects can keep their own Boards open at the same time.

Then go back to Codex, Claude Code, or another local harness and start creating Tasks in natural language.

## What everyday use looks like

Think of Agent Board as a shared task ledger for AI-assisted development:

```text
Codex / Claude Code / OpenCode
        ↓
   create and manage Tasks
        ↓
     Agent Board
        ↓
 Paseo / Orca / another Orchestrator
        ↓
 Developer + Independent Verifier
```

The management side and execution side can use the same harness or completely different ones.

A single Codex setup is enough. With a dedicated Orchestrator such as Paseo or Orca, execution can move further into the background. Independent Tasks can also run in parallel.

## You keep control

Agent Board does not start development just because a Task exists.

- a Task can remain an unqueued draft;
- READY means you have accepted that it may execute;
- unclear product semantics, conflicts, and unexpected conditions should come back to a Human;
- merge / push automation is authorized per run.

The point is not to remove the Human from development. It is to remove the Human from **babysitting the middle**.

## What you see on the Board

The Web Board has four current states:

- `READY` — safe to execute
- `IN_PROGRESS` — being worked on
- `DONE` — completed
- `BLOCKED` — waiting for resolution

Unqueued Tasks are shown separately instead of pretending to be a fifth lifecycle state.

Each Task exposes its content, execution facts, delivery, verification, and audit history. The home page keeps DONE bounded to recent completions, with full paginated history available separately.

## Two Skills, one job each

`aboard skill install` installs two independent Skills:

- **`agent-board-management`** — turn ideas into Tasks, edit them, queue them, and manage priority;
- **`agent-board-workflow`** — tell the execution layer how to coordinate, verify, and deliver work.

You do not need to memorize their rules. In Skill-aware harnesses such as Codex or Claude Code, natural-language use is the normal path.

To inspect them:

```bash
aboard skill check
aboard skill show management
aboard skill show workflow
```

### Upgrade Skills

Regular installation adds missing Skills and preserves installed content that differs. To replace a selected Skill with the version embedded in the current `aboard` binary, use `--force` explicitly:

```bash
aboard skill install --force workflow
```

Omit `workflow` or `management` to apply `--force` to both Skills.

## CLI, Web, and MCP

You can use the Web Board directly, or let agents operate the same Board through CLI or MCP.

Common commands:

```bash
aboard board
aboard task list
aboard ready list
aboard history 12
aboard doctor
```

MCP is optional, not a prerequisite:

```bash
aboard mcp config claude-code
aboard mcp config codex
aboard mcp config opencode
```

## Local-first

No account, service deployment, or separate database server is required.

Board data lives at:

```text
~/.agent-board/<project_id>/board.db
```

The repository only needs the `.agent-board.json` project identity file.

## It does not want to become another giant platform

Agent Board does not own model routing or agent runtime, and it does not require you to replace your IDE, harness, or existing development workflow.

It solves one problem:

> **give task management and task execution a stable, shared, auditable handoff surface.**

Cloud sync, team services, desktop clients, and remote execution can be added by other pieces later. The core Board does not need to grow into a platform.

## More docs

- [PRODUCT.md](PRODUCT.md) — product definition and boundaries
- [ARCHITECTURE.md](ARCHITECTURE.md) — architecture and persistence
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

Apache-2.0.
