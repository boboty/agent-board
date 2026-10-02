# Agent Board

[中文](README.md) | [English](README_EN.md)

**AI work should leave more than an output. It should leave an evidence trail that explains why the output can be accepted.**

Agent Board is a local-first reference implementation of an **AI work handoff and verification discipline**. It keeps tasks, execution facts, delivery evidence, independent verification, and human decisions in an external persistent ledger so work can move reliably across agents, sessions, harnesses, and worktrees.

**The board is only a view. The real core is: rules + work ledger + evidence and verdicts.**

![Agent Board](docs/images/board.webp)

- **External source of truth**: task state does not live only inside one agent's context, todo list, or `PROGRESS.md`
- **Independent verification**: the Developer delivers; a fresh, independent Verifier decides whether the work passes
- **Reviewable evidence**: DONE is not just a column; delivery, verification, and acceptance evidence remain inspectable
- **Harness-independent**: Claude Code / Codex / OpenCode and others can operate on the same work facts through CLI / MCP
- **Local-first**: one binary + SQLite, with no account, hosted service, or database server required

## Why an external work item exists

Agents are getting better at planning, decomposition, self-checking, and managing their own internal context. Those activities can stay inside the harness.

But these facts should not exist only in one agent's memory:

- what the work actually is and what counts as complete
- who picked it up, who executed it, and who took over after an interruption
- what the Developer actually delivered
- what an independent Verifier checked and what evidence supported the result
- why the result was PASS, RC, or BLOCKED
- where a Human authorization or decision was required

These are not primarily intelligence problems. They are problems of **handoff, verification, and responsibility boundaries**.

> **Agents manage their internal steps. The Board records only the shared facts that must survive across executors and sessions.**

## From a governance perspective, four kinds of facts matter

| | Question answered |
|---|---|
| **Task** | What should be done? What counts as complete? |
| **Execution** | Who worked on it? In which harness / worktree? Was there a takeover? |
| **Evidence** | What did the Developer deliver? What did the Verifier actually verify? |
| **Verdict** | PASS, RC, BLOCKED, or a Human decision? |

The implementation still keeps task lifecycle deliberately simple: `READY` / `IN_PROGRESS` / `DONE` / `BLOCKED`.

Developer, Verifier, RC, handoff, and session are not extra top-level task states. They are execution and audit facts around the work item.

## Rules before tools

Agent Board deliberately separates two concerns.

### Workflow Skill: defines the rules

- how Tasks are defined and when they enter READY
- responsibilities of the Orchestrator, Developer, and Independent Verifier
- how delivery moves into independent verification
- how RC flows back and how re-verification works
- how interruption, takeover, and stop-for-decision are handled
- what actually counts as complete

### Shared Task Board: records the facts

- Tasks and explicit task-level state
- READY ordering
- execution metadata such as harness / model / worktree
- delivery evidence
- verification evidence
- blocked reasons
- handoff / takeover facts
- audit history

**Code provides capabilities. The Skill defines the rules. The Board is a ledger, not a workflow engine.**

## How one work item completes

Suppose you are building `shop-api` and find a bug: stacking a discount coupon with a promotion charges too much.

**1. Define the work item first. Do not start yet.**

> Turn this bug into a Board Task with clear acceptance criteria. Do not start implementation yet.

The Task is created for review. Only READY is an explicit authorization to execute.

**2. The Developer executes and leaves evidence**

> Execute the READY queue following `agent-board-workflow`. Stop and report when a Human decision is required.

The Developer may manage its own internal plan, but the delivery must leave reviewable facts and evidence.

**3. An Independent Verifier checks it**

A **fresh, independent session/instance** verifies the Task against its acceptance criteria instead of letting the Developer declare its own success.

- PASS → accept delivery → DONE
- RC → return to the Developer for correction → verify again with a new independent Verifier
- Human decision required → BLOCKED with the reason recorded

![Task detail: delivery, independent verification, and audit history](docs/images/task-detail.webp)

What you come back to is not “the agent says it is done,” but:

**what changed → what evidence exists → who independently checked it → why it was accepted.**

Human attention stays focused on three things: **what to do, what comes first, and what requires judgment.**

## How this relates to GitHub Issues / PRs

Agent Board is not trying to replace GitHub.

GitHub Issues / PRs / CI / Review are excellent for collaboration and closure inside the code-hosting platform. Agent Board focuses closer to the execution surface: **local, multi-worktree, multi-harness, multi-agent execution, handoff, and independent verification facts.**

The semantics map naturally:

- Board Task ↔ Issue / work item
- Developer delivery ↔ implementation / PR candidate
- Verification evidence ↔ CI / review evidence
- PASS / Closure ↔ accepted delivery / merge boundary

If your team can eventually carry the same rules entirely through GitHub or another platform, that is fine. **The rules should remain valid without depending on Agent Board itself.**

Agent Board exists as a lightweight, local reference implementation that agents can operate directly.

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

`aboard init` creates `.agent-board.json`; commit it to the repository. Then return to your AI tool and say something like “turn XX into a Board Task.”

**Choose an execution setup**

- **One Claude Code / Codex is enough**: use separate, independent sessions/instances for Developer and Verifier
- **Background execution or parallel Tasks**: an Orchestrator such as [Paseo](https://github.com/getpaseo/paseo) can consume READY and assign a Developer plus Independent Verifier

If you want merge / push automated as well, authorize that explicitly for the run; otherwise stop at the verified delivery boundary.

## You keep control

- Creating a Task does not start work; READY is the explicit execution boundary
- Unclear semantics, conflicts, and unexpected conditions stop and get recorded, using BLOCKED when necessary
- A Developer cannot replace independent verification with “I checked my own work”
- merge / push automation is authorized by the Human for the run

Agent Board is not about removing Humans from software development. It is about removing Humans from **babysitting the middle** and keeping their attention on authorization and judgment.

## What it deliberately does not do

No model routing. No agent runtime. No agent scheduling. No takeover of an agent's internal todo list. No requirement to replace your IDE or harness.

It is responsible for one thing:

> **when AI work passes between executors, the task, evidence, verification, and decision trail do not disappear with a session.**

## More

- [Everyday use: CLI / MCP / the two Skills](docs/usage_EN.md)
- [Upgrade, migrate, clean, uninstall](docs/operations_EN.md)
- [PRODUCT.md](PRODUCT.md) — product definition and boundaries
- [ARCHITECTURE.md](ARCHITECTURE.md) — architecture and persistence
- [Development from source](docs/development.md)

Apache-2.0
