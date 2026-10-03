# Agent Board

[中文](README.md) | [English](README_EN.md)

**A work protocol for accountable AI software delivery.**

Every meaningful piece of delegated agent work should have a clear owner, an evidence-backed delivery, an independent verification, and a record of why it was accepted.

Agent Board is first a **work discipline for AI-assisted software development**: it defines what deserves to become a Task, who takes responsibility for execution, what counts as delivery, who may independently verify it, and how interruption, rework, and human decisions stay on the record.

The discipline is encoded as a harness-independent Workflow Skill. `aboard` is its local reference implementation, giving different agents, sessions, harnesses, and worktrees the same source of truth.

## The problem isn't intelligence

Models are getting better at planning, decomposition, self-checking, and managing their own context. Those internal steps can stay inside Codex, Claude Code, OpenCode, or any other harness.

Once several agents work across sessions, worktrees, and tools, the hard questions become different:

- What exactly is the work, and what counts as complete?
- Who is working on it now, and who can take over after an interruption?
- The agent says it is done. What evidence makes that acceptable?
- Should the agent that wrote the code be allowed to approve it?
- After RC, what changed and who verified the new delivery?
- Two weeks later, what was delivered, who checked it, and why was it accepted?

Better models do not make these questions disappear. They are problems of **coordination, handoff, verification, and accountability**.

**The core discipline itself is not specific to software development, but software engineering is the first domain it has been designed and validated against in real projects.** Other domains may need different evidence and verification rules; those should be proven separately rather than assumed here.

Human engineering teams use tickets, review, sign-off, and audit trails for the same reason. Agent teams need a written discipline that agents themselves can follow.

## The rules

1. **One independently delegable unit of work, one Task.** Only work that can be independently scheduled, verified, and handed off belongs on the Board. Worker todos, plans, sub-steps, and subagent decomposition stay inside the harness.
2. **Define the boundary before authorizing execution.** A Task needs a clear goal, scope, and checkable acceptance criteria. Creating it does not start work; entering `READY` is the execution boundary. A Task workspace has at most one current Worker at a time.
3. **Done means evidence.** A delivery records what changed, its baseline, checks performed and raw results, what was not verified and why, plus known limitations.
4. **Separation of duties.** The Worker may self-check, but never declares PASS. Every formal verification round uses a new, independent Verifier session/instance and ends in `PASS`, `RC`, or `BLOCKED`.
5. **Rework stays with the Task.** RC returns to the same Task and delivery boundary for the Worker to correct. A correction produces a new delivery and a new Independent Verifier re-verifies the whole Task. Delivery, verification, and handoff facts are append-only.
6. **Shared state does not live in one agent's memory.** On interruption or takeover, recover from the Task and Board facts, Workspace / Git, and agent activity. A handoff fact supplements missing context; it does not create another `PROGRESS.md`.

The Human keeps authority over product intent, READY priority, and outward or irreversible actions such as push, merge, and release. The Orchestrator coordinates but does not perform the work or verify it. The Worker executes and delivers. The Independent Verifier is read-only.

> **Agents manage their internal steps. The protocol governs only the responsibilities, evidence, and decisions that must survive across executors.**

## Reference implementation: `aboard`

Agent Board turns those rules into an external, persistent, auditable task ledger.

**The board is only a view. The real core is: rules + work ledger + evidence and verdicts.**

![Agent Board](docs/images/board.webp)

- **External source of truth**: task state does not live only in one agent's context, todo list, or progress file
- **Independent verification**: a Worker delivers; a fresh Independent Verifier checks it
- **Reviewable evidence**: delivery / verification / handoff / decision history remains inspectable
- **Harness-independent**: CLI / MCP / Web operate the same facts without binding to Claude Code, Codex, OpenCode, or a particular model
- **Local-first**: one binary + SQLite, with no account, hosted service, or database server required

The Task lifecycle stays deliberately small: `READY` / `IN_PROGRESS` / `DONE` / `BLOCKED`.

Worker, Verifier, RC, handoff, and session are not additional top-level states. They are execution and audit facts around the Task.

## What it isn't

Not an IDE. Not an agent. Not a harness. Not a scheduler.

Keep discussing and developing in the tools you already use, and keep using Paseo or another orchestrator if you want parallel execution. Agent Board does not take over an agent's internal plan, route models, or turn the Board into a workflow engine.

**Code provides capabilities. The Skill defines the rules. The Board is a ledger, not a workflow engine.**

## How one Task completes

Suppose you are building `shop-api` and find a bug: stacking a discount coupon with a promotion charges too much.

**1. Define the Task first. Do not start yet.**

> Turn this bug into a Board Task with clear acceptance criteria. Do not start implementation yet.

The Task is created for review. Only READY is an explicit authorization to execute.

**2. The Worker executes and leaves evidence**

> Execute the READY queue following `agent-board-workflow`. Stop and report when a Human decision is required.

The Worker manages its own internal plan, then leaves a stable workspace, delivery facts, and sufficient evidence.

**3. An Independent Verifier checks it**

A **fresh, independent session/instance** verifies the Task against the full diff and its acceptance criteria instead of letting the Worker declare its own success.

- PASS → accept the verified delivery → package the accepted commit → DONE
- RC → return to the Worker → create a new delivery → verify again with a new Independent Verifier
- BLOCKED / Human decision required → stop and record the reason

![Task detail: delivery, independent verification, and audit history](docs/images/task-detail.webp)

What you come back to is not “the agent says it is done,” but:

**what changed → what evidence exists → who independently checked it → why it was accepted.**

Human attention stays focused on three things: **what to do, what comes first, and what requires judgment.**

## How this relates to GitHub Issues / PRs

Agent Board is not trying to replace GitHub.

GitHub Issues / PRs / CI / Review are excellent for collaboration and closure inside the code-hosting platform. Agent Board focuses closer to the execution surface: **local, multi-worktree, multi-harness, multi-agent execution, handoff, and independent verification facts.**

The semantics map naturally:

- Board Task ↔ Issue / work item
- Worker delivery ↔ implementation / PR candidate
- Verification evidence ↔ CI / review evidence
- PASS / Closure ↔ accepted delivery / merge boundary

If your team can eventually carry the same rules entirely through GitHub or another platform, that is fine. **The rules should remain valid without depending on Agent Board itself.**

`aboard` exists as a lightweight, local reference implementation that agents can operate directly.

## Compared with common approaches

| | TODO.md / PROGRESS.md | Agent built-in todo | Issues / Linear | **Agent Board** |
|---|---|---|---|---|
| Shared across sessions / worktrees | Needs conventions; easy to fork | Usually scoped to one session or harness | ✓ | ✓ |
| Source of task state | File contents | Harness / session internal state | Explicit fields or automation | Explicit records with actor + version |
| Evidence behind “done” | You define it yourself | Usually no independent verification trail | Depends on comments / CI / Review | Delivery + independent verification facts |
| Direct agent access | Yes, but concurrent edits can conflict | Usually current harness only | Requires API / auth | CLI / MCP with optimistic versioning |
| Handoff during local execution | Manual convention | Usually not cross-harness | Usually centered on remote Issue / PR | Native handoff / audit facts |
| Deployment | None | None | Account / network / service | Local binary + SQLite |

## Start in 5 minutes

Requirement: **Go 1.25+**.

```bash
go install github.com/boboty/agent-board/cmd/aboard@latest
aboard skill install

cd your-project
aboard init
aboard doctor
aboard web
```

`aboard init` must run inside a Git repository. It writes the project identity to this machine's Git common directory (`.git/agent-board.json`), outside the worktree and never committed. Every worktree of the repository shares the same local Board automatically; an independent clone on another machine runs its own `aboard init` and gets its own Board. Then return to your AI tool and say something like “turn XX into a Board Task.”

**Choose an execution setup**

- **One Claude Code / Codex is enough**: use separate, independent sessions/instances for Worker and Verifier
- **Background execution or parallel Tasks**: an Orchestrator such as [Paseo](https://github.com/getpaseo/paseo) can consume READY and assign a Worker plus Independent Verifier

If you want merge / push automated as well, authorize that explicitly for the run; otherwise stop at the verified delivery boundary.

## You keep control

- Creating a Task does not start work; READY is the explicit execution boundary
- Unclear semantics, conflicts, and unexpected conditions stop and get recorded, using BLOCKED when necessary
- A Worker cannot replace independent verification with “I checked my own work”
- merge / push automation is authorized by the Human for the run

Agent Board is not about removing Humans from software development. It is about removing Humans from **babysitting the middle** and keeping their attention on authorization and judgment.

## More

- [Workflow Skill: full rules](workflow/SKILL.md)
- [Everyday use: CLI / MCP / the two Skills](docs/usage_EN.md)
- [Upgrade, clean, uninstall](docs/operations_EN.md)
- [PRODUCT.md](PRODUCT.md) — product definition and boundaries
- [ARCHITECTURE.md](ARCHITECTURE.md) — architecture and persistence
- [Development from source](docs/development.md)

Apache-2.0
