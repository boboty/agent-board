# Everyday Agent Board usage

For a first install, start with [Install with your agent](install-with-agent_EN.md). This page covers everyday operations; for a step-by-step first Worker → Independent Verifier → DONE walkthrough, see [Complete your first Task](first-task_EN.md).

## Two Skills

```bash
aboard skill install
aboard skill check
aboard skill show management
aboard skill show workflow
```

- `agent-board-management` helps the Human, or an explicit delegate, define, edit, queue, and prioritize Tasks.
- `agent-board-workflow` guides execution across Worker, Independent Verifier, RC, BLOCKED, and delivery.

The Skills are independent. CLI is the baseline adapter; MCP is optional.

## Board states

Agent Board has exactly four task states:

- `READY` — accepted and safe to execute
- `IN_PROGRESS` — being worked on
- `DONE` — completed
- `BLOCKED` — cannot continue until something is resolved

An unqueued Task has no lifecycle state and is shown separately.

## Common CLI commands

Read-only:

```bash
aboard board
aboard task list
aboard ready list
aboard task get 12
aboard history 12
aboard events --task 12
```

CLI mutations require a writer-supplied actor label for traceability; it is not authenticated identity:

```bash
export AGENT_BOARD_ACTOR=human
# or pass the actor flag shown by the command's --help output
```

Create an unqueued Task:

```bash
aboard task create \
  -title "Fix stacked coupon calculation" \
  -description "..." \
  -acceptance "..."
```

A Task enters READY only when the Human, or an explicit delegate, accepts it for execution.

## Web

```bash
aboard web
```

The Web Board chooses an available loopback port. It is the Human-facing UI for browsing, editing, queueing, READY ordering, Facts, and history.

## MCP

MCP is optional. Generate stdio configuration for a supported harness when needed:

```bash
aboard mcp config claude-code
aboard mcp config codex
aboard mcp config opencode
```

## Execution setups

One harness can cover both management and execution, but Worker and Independent Verifier should use separate, independent sessions/instances.

With an Orchestrator such as Paseo or Orca, execution can continuously consume READY and assign a Worker plus a fresh Independent Verifier. Agent Board records Tasks, state, and facts; it does not choose models, harnesses, or scheduling strategy.
