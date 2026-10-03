# Agent Board — Architecture

## Layering

```text
AI Engineering Workflow Skill
        ↓ uses
Agent Board
        ↓ persists through
SQLite + adapters
```

The layers are intentionally one-way.

### Workflow Skill

Owns workflow semantics:

- who may do what
- when a task should move
- how Worker and Verifier cooperate
- how RC, handoff, takeover, and human decisions work

### Agent Board

Owns task recording capabilities:

- create / read / edit task
- queue task
- set explicit task state
- reorder READY
- record facts
- record audit events
- expose MCP / CLI / HTTP views

The Board must not infer lifecycle from lease, attempt, review, or agent availability.

### Infrastructure

Owns technical correctness:

- SQLite persistence
- multi-process write safety
- optimistic concurrency
- idempotency
- migration integrity
- transaction boundaries
- HTTP security
- transport adapters

Infrastructure must not define product workflow semantics.

## Initial domain model

Keep the core deliberately small:

- `Task`
- `TaskFact`
- `TaskEvent`

Potential fact kinds for v0:

- `execution`
- `delivery`
- `verification`
- `handoff`
- `note`

Do not introduce Attempt, ReviewRequest, Gate, Reservation, or workflow-policy domain objects unless real usage proves they are needed.

## Initial Board operations

The first contract should stay close to:

```text
create_task
get_task
list_tasks
update_task
queue_task
set_task_state
reorder_ready
record_fact
list_events
```

These are capabilities, not workflow rules.

For example, `set_task_state` records a requested state change reliably. Whether a Worker, Verifier, Orchestrator, or human should invoke it in a particular situation is defined by the Workflow Skill.

## SQLite direction

The thin store should preserve the useful engineering discipline learned from the rhizome POC:

- WAL
- `BEGIN IMMEDIATE` for writes
- busy timeout / BUSY retry
- optimistic version checks
- idempotency keys for retryable writes
- mutation + audit event in one transaction
- READY reorder in one transaction
- a shared DB location outside individual worktrees

Selective infrastructure porting from the POC is allowed. Rhizome workflow semantics are not.

## Context reset rule

When taking over an existing project or building a new product from an open-source base:

> **Existing code is evidence, not authority.**

Before implementation, explicitly identify:

- inherited infrastructure
- inherited constraints
- legacy semantics that are not authoritative
- the current product and architecture authority

> **Forks inherit implementation, not intent.**
