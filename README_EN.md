# Agent Board

[中文](README.md) | [English](README_EN.md)

**“Done” isn’t enough. An independent verifier should say PASS.**

A local-first task board for Claude Code / Codex / OpenCode: queue work, let one agent implement it, let another independently verify it, and keep an auditable trail. You come back only when a human decision is needed.

![Agent Board](docs/images/board.webp)

- **Stop babysitting progress**: task state, delivery, and verification live on the Board across sessions and worktrees
- **Verification leaves evidence**: DONE shows who delivered, who independently verified, and what was checked
- **Bring your own tools**: CLI / MCP / Web operate the same Board; management and execution can come from different harnesses
- **Local-first**: one binary + SQLite, with no account, service, or database server to deploy

## What a complete run looks like

Suppose you are building `shop-api` and find a bug: stacking a discount coupon with a promotion charges too much.

**1. Say one thing in Claude Code / Codex**

> Turn this bug into a Board Task with clear acceptance criteria. Do not start implementation yet.

The Task appears unqueued. Review it, then queue it into READY.

**2. Let the execution side work**

> Execute the READY queue following `agent-board-workflow`. Stop and report when a Human decision is required.

Developer implements → a **fresh, independent session/instance** acts as Verifier → PASS → accepted commit → DONE.

**3. Come back to results, not the middle**

![Task detail: delivery, independent verification, and audit history](docs/images/task-detail.webp)

- the coupon Task records Developer delivery, Independent Verifier PASS, and the verification evidence
- the Redis migration is BLOCKED because a Human must confirm the downtime window — that is where your attention belongs

You keep three jobs: **what to do, what comes first, and what requires judgment.**

## Compared with what you may use today

| | TODO.md / PROGRESS.md | Agent built-in todo | Issues / Linear | **Agent Board** |
|---|---|---|---|---|
| Shared across sessions / worktrees | Needs conventions; easy to fork | Usually scoped to one session or harness | ✓ | ✓ |
| Source of task state | File contents | Harness / session internal state | Explicit fields or automation | Explicit records with actor + version |
| Evidence behind “done” | You define it yourself | Usually no independent verification trail | Depends on comments / automation | Delivery + independent verification facts |
| Direct agent access | Yes, but concurrent edits can conflict | Usually current harness only | Requires API / auth | CLI / MCP with optimistic versioning |
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

`aboard init` creates `.agent-board.json`; commit it to the repository. Then go back to your AI tool and say something like “turn XX into a Board Task.”

**Choose an execution setup**

- **One Claude Code / Codex is enough**: Agent Board still works; keep Developer and Verifier in separate, independent sessions/instances
- **Background execution or parallel Tasks**: an Orchestrator such as [Paseo](https://github.com/getpaseo/paseo) can consume READY and assign a Developer plus Independent Verifier

If you want merge / push automated as well, authorize that explicitly for the run; otherwise stop at the verified delivery boundary.

## You keep control

- Creating a Task does not start work; READY is the explicit execution boundary
- Unclear semantics, conflicts, and unexpected conditions come back to a Human and can be recorded as BLOCKED
- merge / push automation is authorized per run

Agent Board is not about removing Humans from software development. It is about removing Humans from **babysitting the middle**.

## What it deliberately does not do

No model routing. No agent runtime. No agent scheduling. No requirement to replace your IDE or harness. It does one thing:

> **provide a stable, shared, auditable handoff surface between task management and task execution.**

## More

- [Everyday use: CLI / MCP / the two Skills](docs/usage_EN.md)
- [Upgrade, migrate, clean, uninstall](docs/operations_EN.md)
- [PRODUCT.md](PRODUCT.md) — product definition and boundaries
- [ARCHITECTURE.md](ARCHITECTURE.md) — architecture and persistence
- [Development from source](docs/development.md)

Apache-2.0
