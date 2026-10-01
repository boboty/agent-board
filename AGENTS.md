# Agent Instructions

Read these files before changing the project:

1. `PRODUCT.md`
2. `ARCHITECTURE.md`
3. `workflow/SKILL.md`

## Authority

Agent Board is a clean implementation with its own product semantics.

The repository `boboty/agent-board-rhizome-poc` is a reference implementation only. Its guides, skills, status machine, attempt/review workflow, and naming are not authoritative for this repository.

When selectively reusing code from the POC, classify it first:

- **Infrastructure** — may be ported when useful.
- **Domain/workflow semantics** — do not port unless explicitly accepted by current Agent Board specifications.

## Core boundary

> **Code provides capabilities. The Skill defines the rules.**

Do not encode workflow policy into Board code merely because the Workflow Skill requires a behavior.

Do not infer Task state from agent runtime, leases, attempts, review outcomes, or historical heuristics. Task-level state is an explicit recorded fact.

## Engineering discipline

- Keep the domain small.
- Prefer the simplest implementation that satisfies the current Task.
- Do not introduce speculative abstractions or guardrails.
- Preserve attribution and license requirements for selectively reused third-party code.
- Treat tests as verification of current requirements, not as authority over current product intent.
- If existing code conflicts with PRODUCT / ARCHITECTURE / Workflow Skill, stop and surface the conflict instead of silently preserving legacy semantics.
