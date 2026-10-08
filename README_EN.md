# Agent Board

[中文](README.md) | [English](README_EN.md)

**A work protocol for accountable AI software delivery.**

Agent Board defines the responsibilities that need to survive beyond any one agent session: what the Task is, who owns execution, what was actually delivered, who independently verified it, and what a Human ultimately chose to accept.

`aboard` is the local reference implementation. It does not manage how agents think or plan internally; it keeps the durable execution facts that need to survive across sessions, worktrees, and harnesses.

## Why a protocol is needed

Modern coding agents are increasingly good at planning, decomposition, self-checking, and managing their own context. Those internal mechanics can stay inside Codex, Claude Code, OpenCode, or another harness.

The harder problems begin when work crosses agent and session boundaries:

- What exactly is the Task, and what counts as complete?
- Who owns it now, and who may take over after an interruption?
- When an agent says “done,” what evidence supports that claim?
- Did the verifier inspect the same delivery the worker intended to hand off?
- Can the agent that produced the work approve its own result?
- After rework, what changed and who verified the new delivery?
- Two weeks later, can you reconstruct what was accepted and why?

Stronger models do not remove these questions. They are questions of **ownership, handoff, verification, and decision authority**.

The protocol is not inherently limited to software development, but software engineering is the first domain where Agent Board has been exercised against real project work. Other domains may require different evidence and verification rules.

## A Task from definition to acceptance

Suppose `shop-api` has a bug where stacking two discounts overcharges a customer.

### 1. Define the Task before starting work

> Create a Board Task for this bug with clear acceptance criteria. Do not start implementation yet.

Creating a Task is not authorization to execute it. Entering `READY` is.

### 2. A Worker delivers a concrete result

> Execute the READY queue following `agent-board-workflow`. Stop when a Human decision is required.

The Worker manages its own internal plan. When it is done, it leaves a stable workspace, delivery evidence, and an identity for the concrete result being handed off.

### 3. A fresh Independent Verifier checks that delivery

The Independent Verifier evaluates the acceptance criteria, the full diff, and checks it runs itself.

The Worker's delivery notes are **claims to verify**, not evidence to trust by default.

- `PASS` — the delivery passed independent verification
- `RC` — return to the same Task for correction, produce a new delivery, and verify again with a fresh Verifier
- `BLOCKED` — verification cannot complete; record why and stop

These values are verification verdicts, not additional Task states, and they do not automatically move the Task.

At the end of verification, the Verifier preserves needed evidence before removing disposable resources created for that run. After an interruption, the Orchestrator resumes cleanup only after confirming the old Verifier has stopped. The shared delivery workspace remains available for RC correction or PASS packaging.

A Human may explicitly accept a delivery without Verifier `PASS`. That is recorded as a `decision`, never represented as a verification result.

![Task detail: delivery, independent verification, and fact history](docs/images/task-detail.webp)

The result is more useful than “the agent says it is done”:

**what changed → which delivery is being considered → who independently checked it → why it was accepted.**

## The rules

1. **One independently delegable unit of work, one Task.** Work belongs on the Board when it can be independently scheduled, verified, or handed off. Worker todos, plans, sub-steps, and subagent decomposition stay inside the harness.
2. **Define the boundary before authorizing execution.** A Task needs a clear goal, scope, and checkable acceptance criteria. Creation does not start work; `READY` is the execution boundary. A Task workspace has at most one current Worker at a time.
3. **A delivery needs evidence and identity.** “Done” is not a result. The Worker records what changed, the baseline, checks performed, known gaps, and which concrete result is being delivered.
4. **The producer does not approve the work.** A Worker may self-check but cannot declare `PASS`. Formal verification comes from a fresh, independent session or instance and is based on the acceptance criteria, full diff, and the Verifier's own checks.
5. **Verification and Human acceptance are different facts.** `PASS` belongs to the Independent Verifier. A Human can accept a non-PASS delivery, but that choice is recorded as a `decision`.
6. **Rework stays with the Task.** `RC` returns to the same Task and delivery boundary. The correction produces a new delivery and a fresh Independent Verifier checks it again.
7. **Shared state does not live in one agent's memory.** On interruption or takeover, recover from the Task and Board facts, Workspace/Git, and relevant agent activity. Handoff facts fill context gaps; they do not create a second progress system.

The Orchestrator coordinates execution and recovery but does not implement the Task or independently verify it. The Worker executes and delivers. The Independent Verifier does not modify the delivery; it verifies and records a verdict.

The Human retains authority over product intent, READY priority, and outward or irreversible actions such as merge, push, and release.

## Reference implementation: `aboard`

Agent Board turns the protocol into a local, persistent, traceable task ledger.

**Code provides capabilities. Skills define rules. The Board stores facts.**

![Agent Board](docs/images/board.webp)

- **External task facts** — task state does not depend on one agent's context, todo list, or progress file
- **Delivery-to-verification linkage** — structured delivery and verification fields make it possible to determine mechanically which delivery was verified
- **Independent verification records** — Worker delivery, Verifier verdict, and Human decision remain separate facts
- **Traceable provenance** — facts can carry role, session, harness, and model; `actor` identifies the actual writer, not the authorizing Human, and is not authenticated identity
- **Harness-independent access** — CLI, MCP, and the Web Board operate on the same facts without binding the workflow to Claude Code, Codex, OpenCode, or a specific model
- **Local-first deployment** — one binary plus SQLite; no account, hosted service, or database server required

Facts written through `aboard` use append-only semantics. Agent Board is not tamper-evident storage: a process with local file access can still modify the SQLite database directly.

The Task lifecycle stays deliberately small:

- `READY`
- `IN_PROGRESS`
- `DONE`
- `BLOCKED`

Worker, Verifier, RC, handoff, and session are execution facts around a Task, not additional top-level states.

## What Agent Board is not

Agent Board is not an IDE, an AI agent, an agent harness, or a scheduler.

It does not own a Worker's internal plan, select models, launch agents automatically, or infer Task transitions from `PASS` or `RC`.

Use the tools you already work in. Pair Agent Board with Paseo, Orca, or another orchestrator when you want parallel or background execution. Harness-specific operating patterns stay outside the core protocol.

## Relationship to GitHub Issues and Pull Requests

Agent Board is not a replacement for GitHub.

Issues, pull requests, CI, and review are strong tools for collaboration and closure on the code-hosting side. Agent Board operates closer to the local execution surface: **multi-worktree, multi-harness, multi-agent execution, handoff, and independent verification before or alongside remote collaboration.**

If a team can carry the same protocol entirely in GitHub or another system, that is fine. The protocol should remain useful without depending on Agent Board itself.

`aboard` exists as a lightweight local implementation that agents can operate directly.

## Compared with common approaches

| | Files / agent-local todo | Issues / PRs | **Agent Board** |
|---|---|---|---|
| Shared facts during local multi-agent work | Convention-based; easy to diverge | Primarily remote collaboration | Native |
| Cross-harness execution and handoff | Usually tool-local | Requires integration | Native records |
| Independent verification as a local execution fact | Must be designed separately | Can be implemented through PR / Review | Native record |
| Direct agent access | Files or harness APIs | API + authentication | CLI / MCP |
| Runtime dependency | None | Remote service | Local binary + SQLite |

## Recommended: install with your agent

Copy the instructions below into Codex, Claude Code, or OpenCode. The agent will confirm the current OS, harness, and target Git repository, choose an available existing installation route, and report each installation and diagnostic result. The CLI is enough to get started; MCP is optional.

```text
Follow Agent Board's “Install with your agent” guide for this environment:
https://github.com/boboty/agent-board/blob/main/docs/install-with-agent_EN.md

First confirm the current OS/architecture, the harness you are running in, and the Git repository path I want initialized. Do not scan unrelated projects or modify other harness configurations. Choose the suitable existing prebuilt release or Go installation route from the guide. If a prerequisite is missing, this environment is unsupported, or `aboard` is not on PATH after installation, stop and report what succeeded, what failed and why, and the next step. Do not describe a partial setup as complete.

Install both the management and workflow Skills and check their status. If existing Skill content differs, preserve it and report the difference; do not overwrite it with `--force` without my explicit authorization. Run `aboard init` only in the Git repository I confirm, then run `aboard doctor`. Finish with the aboard version and path, actual status of both Skills, project initialization status, doctor result, unresolved issues, and how I can define a Task. Do not create or queue a Task, execute work, commit, push, merge, or release on my behalf.
```

See [Install with your agent](docs/install-with-agent_EN.md) for the complete steps, failure handling, and first-use guide. After installation, continue with [everyday use and the first Worker → Independent Verifier cycle](docs/usage_EN.md).

## Manual installation (fallback)

If you are not using an agent, install manually as follows.

### Download a release binary

Download the `aboard` archive for your platform from [GitHub Releases](https://github.com/boboty/agent-board/releases) and place the binary on your `PATH`.

Current release builds cover:

- macOS — Apple Silicon and Intel
- Linux — amd64 and arm64
- Windows — amd64

Releases also include `SHA256SUMS`.

Then:

```bash
aboard version
aboard skill install

cd your-project
aboard init
aboard doctor
aboard web
```

### Or install with Go

With **Go 1.25+**:

```bash
go install github.com/boboty/agent-board/cmd/aboard@latest
aboard skill install

cd your-project
aboard init
aboard doctor
aboard web
```

`aboard init` runs inside a Git repository. Project identity is stored in the local Git common directory (`.git/agent-board.json` for a normal clone), outside the worktree and never committed. Board data lives under `~/.agent-board/<project_id>/board.db`.

All worktrees of the same local repository share that Board automatically. A separate clone on another machine runs its own `aboard init`.

**Upgrading from v0.1.x?** Project identity storage changed. Read the [upgrade and operations notes](docs/operations_EN.md) first.

Then return to your AI tool and ask it to create or execute Board Tasks.

### Choose an execution setup

- **One Claude Code / Codex / OpenCode instance is enough** — use separate sessions or instances for the Worker and Independent Verifier
- **Parallel or background execution** — use Paseo, Orca, or another Orchestrator to consume READY Tasks and assign Workers and fresh Verifiers

Merge, push, and release automation remains an explicit Human authorization for each run.

## Human control remains explicit

- Creating a Task does not authorize execution; `READY` does
- A Worker's self-check cannot substitute for independent verification
- The Independent Verifier does not modify the delivered work
- A verification verdict does not automatically change Task state
- Human acceptance is not represented as Verifier `PASS`
- Ambiguity, conflicts, and unexpected conditions stop and get recorded; use `BLOCKED` when appropriate
- Merge, push, release, and other outward actions require explicit Human authorization for the run

Agent Board is not about removing Humans from software delivery. It is about moving Human attention away from babysitting execution and back to **intent, priority, and judgment**.

## More

- [Workflow Skill: full rules](workflow/SKILL.md)
- [Everyday use: CLI / MCP / Skills](docs/usage_EN.md)
- [Upgrade, clean, uninstall](docs/operations_EN.md)
- [PRODUCT.md](PRODUCT.md) — product definition and boundaries
- [ARCHITECTURE.md](ARCHITECTURE.md) — architecture and persistence
- [Development from source](docs/development.md)

Apache-2.0. Selected infrastructure code is derived from [rhizome-mcp](https://github.com/Odrin/rhizome-mcp) under Apache-2.0; see [NOTICE](NOTICE).
