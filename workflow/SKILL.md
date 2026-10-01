# Agent Board AI Engineering Workflow Skill

## Status

This is the initial workflow authority skeleton. Detailed procedures will be extracted and refined from the existing engineering standards.

## Purpose

Provide a harness-independent AI engineering workflow using Agent Board as the shared task ledger.

## Core rule

> **Code provides capabilities. The Skill defines the rules.**

Agent Board exposes operations. This Skill defines which role should use them, when, and why.

## Roles

### Orchestrator

Owns task-level coordination.

Responsibilities include:

- select and start Tasks
- dispatch work
- interpret Developer delivery
- request independent verification
- route RC back for correction
- handle handoff / takeover
- stop for human decision when required
- record final task-level lifecycle changes

The Orchestrator coordinates; it does not write implementation code.

### Developer

Owns implementation.

Responsibilities include:

- implement the assigned Task
- self-check the implementation
- return delivery and evidence
- report blocked or handoff information

The Developer does not independently redefine Task lifecycle policy.

### Independent Verifier

Owns independent verification.

Responsibilities include:

- verify the delivered result against Task acceptance criteria
- inspect actual implementation and evidence
- return PASS / RC / BLOCKED with evidence
- remain independent of the implementation work

The Verifier does not modify implementation code.

## Task-level states

Only:

- `READY`
- `IN_PROGRESS`
- `DONE`
- `BLOCKED`

RC, verification, handoff, and agent runtime are execution details within a Task, not additional Board states.

## Normal workflow

```text
READY
  ↓ Orchestrator starts
IN_PROGRESS
  ↓ Developer delivers
  ↓ Independent Verifier checks
      ├─ PASS → Orchestrator records DONE
      └─ RC   → correction by Developer → new independent verification
```

During normal RC and re-verification, the Task remains `IN_PROGRESS`.

If execution cannot continue, the Task may be recorded as `BLOCKED` with sufficient reason and handoff context.

## Board operations

The Skill will map workflow actions to stable Board operations such as:

- create task
- queue task
- set task state
- record execution fact
- record delivery fact
- record verification fact
- record handoff fact
- query task and history

Adapters may expose those operations through MCP, CLI, or HTTP. The workflow must not depend on a specific harness.

## Scope discipline

Board Tasks are task-level work units. Internal implementation plans, todo lists, function-by-function steps, and subagent decomposition remain inside the executing agent/harness unless they independently require scheduling, verification, or handoff.

## Detailed sections to complete

- Task definition and readiness
- role × Board-operation matrix
- Developer delivery contract
- Independent Verifier contract
- RC and re-verification
- handoff and takeover
- stop-for-decision
- Orchestrator recovery
- concrete MCP / CLI command mapping
