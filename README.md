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

## License

Apache-2.0. Third-party code selectively reused from other projects must retain the required attribution and license notices.
