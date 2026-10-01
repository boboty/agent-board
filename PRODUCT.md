# Agent Board — Product Definition

## Purpose

Agent Board provides a shared, repository-independent task ledger for AI-assisted software delivery.

It exists because repository-local progress files and internal agent todo lists are poor shared sources of truth across multiple harnesses, sessions, and worktrees.

## Product boundary

Agent Board has two separate concerns:

### 1. Workflow Skill

The Workflow Skill defines how work should progress:

- Task definition
- Orchestrator responsibilities
- Developer responsibilities
- Independent Verifier responsibilities
- delivery
- verification
- RC / correction
- handoff / takeover
- stop-for-decision
- completion

The Skill is harness-independent.

### 2. Shared Task Board

The Board records shared facts:

- tasks
- explicit task-level state
- READY ordering
- execution metadata
- worktree / harness / model
- delivery evidence
- verification evidence
- blocked reason
- handoff facts
- audit history

The Board does not schedule agents and does not decide workflow.

## Task-level state

The visible task lifecycle has exactly four states:

- `READY`
- `IN_PROGRESS`
- `DONE`
- `BLOCKED`

Developer, Verifier, RC, handoff, review, lease, and session are not top-level task states.

A task may exist before it is queued. That is represented separately from task state, for example by queue metadata such as `queued_at`; it is not a fifth Board column.

Archival/cancellation is also orthogonal to lifecycle state and should not become a fifth Board state.

## Product principles

1. **Code provides capabilities. The Skill defines the rules.**
2. **The Board is a ledger, not a workflow engine.**
3. **Task state is recorded explicitly, not inferred from runtime activity.**
4. **Runtime facts are details, not lifecycle authority.**
5. **The Board manages Tasks; agents manage internal steps.**
6. **Only promote work to a Board Task when it can be independently scheduled, verified, or handed off.**
7. **Add enforcement only for observed failure modes, not hypothetical ones.**
8. **Existing code is evidence, not authority.**
9. **Forks inherit implementation, not intent.**
