---
name: agent-board-workflow
description: Harness-independent AI engineering workflow for Orchestrator, Worker, and Independent Verifier roles using Agent Board as the shared task ledger. Load it whenever you define, start, implement, verify, hand off, block, or close an Agent Board Task.
---

# Agent Board Workflow Skill

## Purpose

This Skill defines how people and agents deliver Tasks recorded on Agent Board: what a well-defined Task is, who does what, when the Task-level state changes, how delivery is independently verified, and how work survives interruption.

> **Code provides capabilities. The Skill defines the rules.**

The Board records whatever it is asked to record. It does not check roles, enforce transitions, or run this workflow. Every rule below is followed by the roles themselves; none of it should be pushed into Board code.

The Skill does not depend on a harness, model, or product. Roles are responsibilities, not tools. Any harness that can run separate agent sessions and reach the Board through MCP, the CLI, or the Web Board can follow it. The execution backend for a run is whatever the Human or the current run names; do not infer it from history or go looking for other platforms.

The Skill constrains **power boundaries, irreversible risk, and acceptance results**. It does not script every step. Handle what can be judged reliably in context; add rules only for failures that actually recur.

## Where things live

- **Task** (on the Board) defines the work: title, description, acceptance criteria.
- **Task-level state** (on the Board) says where the Task stands: `READY`, `IN_PROGRESS`, `DONE`, `BLOCKED`.
- **Facts** (on the Board) carry what happened: execution, delivery, verification, decision, handoff, notes.
- **Workspace / Git** holds the delivered work itself.
- **Agent activity** (transcripts, session logs, the harness's own records) holds the process.

Together these are enough to resume work. Do not keep a `PROGRESS.md` or another repository-local status file. When a handoff needs context that the records above do not show, write it in a `handoff` fact. That fact supplements those records; it does not replace them.

### What becomes a Board Task

Only work that can be **independently scheduled, verified, and handed off** — work worth a full Worker → Independent Verifier cycle.

Worker todos, plans, sub-steps, and subagent decomposition stay inside the Worker's harness. They do not go on the Board. If a Worker finds work outside the current Task, it reports it; it does not create a Task for it.

## Roles

### Human

The authority over product intent and the Board's agenda.

- Defines, accepts, and changes Tasks; sets READY priority.
- Decides everything listed under [Stop for decision](#stop-for-decision).
- Authorizes push, merge, release, and other outward or irreversible actions unless the project explicitly delegates them.
- May act in any role, or delegate Task definition and prioritization to a named agent. The delegate then acts with that authority, within its stated limits.

### Orchestrator

Coordinates one or more Tasks within their current definition.

- Selects the next Task, checks that it is ready, and starts it.
- Launches the Worker, continues it, or replaces it.
- Decides when a delivery is ready for verification and launches a **new** Independent Verifier each time.
- After Verifier PASS, packages the verified workspace as the accepted commit and continues repository integration (see [Verifier PASS → DONE](#4-verifier-pass--done) and [Closure](#closure)). Commit packaging is a Git delivery operation; it does not write implementation or verify it.
- Routes RC back to the Worker.
- Records Task-level state: start, DONE, BLOCKED, resume.
- Stops for a Human decision when needed.

The Orchestrator does not write implementation, does not verify, and does not change a Task's goal, scope, dependencies, or acceptance criteria.

### Worker

The **only writer** of the implementation for its Task.

- Implements the Task within its definition and self-checks: derives the check scope from the Task's acceptance boundary and plausible impact radius, and gathers the smallest sufficient evidence for correctness and safety. Repository-wide build, test, vet, or e2e checks are not default actions. Expand validation when the change could affect a broader area, available evidence leaves a concrete uncertainty, or the Task explicitly requires it. A check unrelated to the Task, or a slow or failing check, does not automatically become a delivery gate; assess any failure against the affected behavior and acceptance criteria.
- Delivers a stable, identifiable workspace (see [Git delivery boundary](#git-delivery-boundary)) along with evidence and limitations.
- Reports blockers, scope questions, and out-of-scope findings to the Orchestrator.

The Worker never declares PASS. Its self-review, and any reviewer or subagent it launches, are development checks, not Independent Verification.

### Independent Verifier

Judges a delivery independently and returns `PASS`, `RC`, or `BLOCKED` with evidence.

- **Independent** means a different person from the Worker, or for AI, a separate session with its own context. It is not the Worker, not a subagent of the Worker, and not the Orchestrator.
- **New** means each verification round gets a fresh Verifier. A Verifier never re-verifies a delivery it has already ruled on.
- **Read-only** means it does not change code, tests, Task content, or project documents, and does not commit. Running checks that write only local, untracked artifacts is fine.

#### Verification resources

The Orchestrator and Verifier keep the verification run's temporary resources identifiable by exact path and owner. This includes any temporary clone or worktree, test database, build or test output, log, or download created for that run. Use a run-specific location; do not search `/tmp` or remove paths whose ownership is uncertain.

Before removing a resource, save the evidence needed to understand the verdict to a durable location, such as a persistent run log. The Verifier removes the disposable resources it created, checks that the delivery fingerprint is unchanged, then records and returns the verdict with the cleanup outcome. Cleanup must not modify the delivered workspace or its baseline/fingerprint. Keep the Worker's workspace available for RC correction and for the Orchestrator's PASS packaging. Remove a temporary Git worktree with `git worktree remove` so Git's worktree metadata is updated; do not substitute directory deletion.

Apply the same cleanup responsibility to `PASS`, `RC`, and `BLOCKED`. For `RC`, retain the shared delivery workspace while removing only the Verifier's disposable resources. For `BLOCKED`, retain a resource only when it is needed to resume; record its exact path, why it must remain, who owns cleanup, and what event or condition will trigger it. Report cleanup failures with the affected paths and errors; do not describe cleanup as complete when it failed.

If the Verifier is interrupted, times out, or exits before cleanup, the Orchestrator records the known resource paths in the handoff context. It confirms the old Verifier can no longer write before removing anything. If that cannot be confirmed, leave the resources in place and resume cleanup only after write authority is resolved. Then remove only the recorded, run-owned resources, preserve any needed evidence first, and report any cleanup failure. An interrupted verification has no verdict. If cleanup changes the delivery fingerprint, stop: the evaluation has no valid verdict and a fresh Independent Verifier must check the changed delivery.

### Role separation

One session holds one role for a given Task. The Worker and the Verifier of a Task are never the same session, and neither is the Orchestrator. One harness may host several roles as separate sessions.

At any moment a Task's workspace has **at most one current Worker**. The Orchestrator maintains this; the Board does not need to record it.

## Task definition and readiness

A Task is defined when its Board fields let a Worker and an independent Verifier work from them without asking the author:

- **title**: the outcome in a few words.
- **description**: goal; scope; what must not change; inputs and outputs; constraints; dependencies on other Tasks; project-specific exceptions and their reasons.
- **acceptance_criteria**: concrete, checkable conditions, including the minimum evidence needed to establish correctness and safety in proportion to the expected change radius and risk, and real API/UI/database checks when they apply. Do not require re-proving existing system guarantees outside the affected scope. "Works correctly" is not a criterion.

A Task is **ready** when, in addition:

- no open product or architecture decision blocks it;
- its dependencies are DONE, or the description says why it can proceed anyway;
- it can be verified independently, and is worth doing so.

Creating a Task (`create_task`) makes an **unqueued** draft with no state. **Queueing** it (`queue_task` → `READY`) is the act of accepting the definition as ready. Queueing belongs to the Human or their delegate. An Orchestrator may draft unqueued Tasks, for example follow-up work, but does not queue them.

## Task-level state

The Board has exactly four states. This Skill uses them as follows:

| State | Meaning | Entered by |
|---|---|---|
| `READY` | Defined, accepted, waiting to start, in priority order | `queue_task` by Human / delegate |
| `IN_PROGRESS` | Started; development, delivery, verification, RC, correction, and takeover all happen here | Orchestrator, at start or on resume |
| `BLOCKED` | Cannot continue without input that will not arrive in the current run | Orchestrator (or Human) |
| `DONE` | An Independent Verifier PASS is recorded and the accepted commit holds exactly the verified content, or an explicit Human acceptance is recorded as a `decision` fact | Orchestrator (or Human) |

Transitions used by the Skill:

```text
(unqueued) ──queue──▶ READY ──start──▶ IN_PROGRESS ──Verifier PASS──▶ DONE
                                         │    ▲
                                    block│    │resume
                                         ▼    │
                                        BLOCKED

IN_PROGRESS ──Human acceptance (`decision` fact)──▶ DONE
```

- RC, verification, review, handoff, takeover, and "waiting for a decision" are **process facts inside a state**, not states. Never invent a fifth state, and never encode one in a reason string.
- A started Task that resumes goes `BLOCKED → IN_PROGRESS`. It does not go back to `READY` or get re-created; its history and work carry over.
- The Board accepts any transition. Moves outside the diagram, such as reopening `DONE` or returning a started Task to `READY`, are Human decisions. By default, a defect found after DONE becomes a new Task.

## Board usage contract

### Role → operation matrix

| Operation | Human | Orchestrator | Worker | Independent Verifier |
|---|---|---|---|---|
| `get_task`, `list_tasks`, `list_ready`, `list_facts`, `list_events` | read | read | read | read |
| `create_task` | define Tasks | unqueued drafts only (follow-ups, proposed splits) | — (report instead) | — |
| `update_task` | change definition | only to record a definition change the Human decided | — | — |
| `queue_task` | accept as ready | — | — | — |
| `reorder_ready` | set priority | — | — | — |
| `set_task_state` | any, as a decision | start, DONE, BLOCKED, resume | — | — |
| `record_fact` `execution` | — | launches and replacements of roles | — | — |
| `record_fact` `delivery` | — | accepted commit after PASS; on behalf of a Worker without Board access | each delivery | — |
| `record_fact` `verification` | — | on behalf of a Verifier without Board access | — | each verdict |
| `record_fact` `decision` | Human decisions, including acceptance | — | — | — |
| `record_fact` `handoff` | — | when taking over or pausing | when stopping with work unfinished | — |
| `record_fact` `note` | — | clarifications, reasons | as needed | as needed |

A dash means the role does not use the operation in this workflow. The Board will not stop it; the role simply does not do it. Only an Independent Verifier records a `verification` verdict; a Human records acceptance as a `decision` fact. Neither fact changes Task state.

### Facts

Facts are append-only and never change state. Write the body for a reader who has nothing else, such as a replacement agent or the Human. Put a few machine-readable keys in `data` when they help. The Board stores `data` without interpreting it. `provenance` is a separate structured field, not part of `data`.

| Kind | Body should say | Structured fields and useful `data` |
|---|---|---|
| `execution` | which role was launched or replaced, and where | provenance: `role`, `session`, `harness`, `model`; `worktree`, `branch`, `baseline` in `data` |
| `delivery` | what changed; checks run with raw results; what was not verified and why; limitations; known out-of-scope findings. After PASS, explain that the accepted commit holds exactly the verified content | structured fields: `baseline`, `fingerprint`, optional `accepted_commit`; useful `data`: `branch`, checks and other evidence; provenance: `role`, `session`, `harness`, `model` |
| `verification` | evidence per acceptance criterion; what was not verified and why; for RC each issue with evidence; for BLOCKED what stops verification | structured fields: `verdict` (`PASS`/`RC`/`BLOCKED`), `baseline`, `fingerprint`; useful `data`: round and other evidence; provenance: `role`, `session`, `harness`, `model` |
| `decision` | a Human decision, including explicit acceptance when no Verifier PASS is recorded | `decision` (for example `accept`), `reason` |
| `handoff` | what is done, what remains, workspace state, what the next person must check first, where the agent activity is | provenance: `role`, `session`, `harness`, `model`; `worktree`, `branch`, `baseline`, `checkpoint` |
| `note` | a clarification, a reason, or context that is not a Human decision | — |

For delivery and verification, the structured fields are the source of truth for these core values; do not put their only copy in `data` or the body. `data` remains available for evidence, checks, limitations, branch, round, and other extensible details. Historical facts can lack structured core fields and must remain missing; do not infer them from old bodies or arbitrary data. Record every delivery and every Verifier verdict, including RC verdicts. They are what make takeover and re-verification possible without a progress file.

### Conventions

- **actor**: a self-reported trace label, for example `orchestrator/codex`, `worker/codex-7f3a`, or `human/yan`. It is not authenticated identity. Keep it for compatibility and human-readable audit history; do not parse it for role or session.
- **fact provenance**: agent-generated `execution`, `delivery`, `verification`, and `handoff` facts record the available `role`, `session`, `harness`, and `model` as structured provenance. These values are self-reported metadata, not authenticated identity or authorization. Human facts may omit agent provenance or record whichever fields are available. Missing historical provenance stays missing; never infer it from actor, body, or neighboring facts.
- **expected_version**: use the version you just read. On `VERSION_CONFLICT`, re-read the Task and its recent facts, decide whether your change still makes sense, then retry. Never retry blindly.
- **idempotency_key**: use one when retrying a mutation whose outcome you could not observe, e.g. `AB-12-done` or `AB-12-verify-r3`. Reuse the same key only for the identical request.
- **reason** on `set_task_state` is a short human-readable line. It is required in practice for `BLOCKED`; omit it when resuming to clear it. Details belong in a fact.
- Humans use the same operations through the Web Board or CLI. Input validation is the same in both adapters.

## Git delivery boundary

**The Task is the delivery boundary; RC is not.** Git records only the Task's verified delivery.

- The Task has a **baseline**: the commit its workspace starts from.
- Until PASS there is **no implementation commit**. Development, self-check, and every RC correction happen in the same uncommitted workspace. The Verifier verifies the full diff of that stable workspace against the baseline.
- A delivery is identified by the baseline plus a **fingerprint** of that diff, including untracked files. Any method that reliably detects a change will do (a hash of `git diff --binary <baseline>` plus untracked file contents, for example). State the method in the `delivery` fact.
- After PASS, the Worker changes nothing more. The Orchestrator packages the unchanged verified workspace once as the Task's accepted implementation commit. Before packaging, it confirms the workspace still matches the PASS baseline and fingerprint. After packaging, it confirms the accepted commit's diff from the baseline matches the PASS fingerprint and the workspace is clean. If either confirmation fails, the commit is not accepted and the Task does not become DONE. Any content change is a new delivery: the Worker makes it, and a new Independent Verifier checks it.
- **Checkpoint commits** are allowed only when there is a real recovery risk across sessions, across machines, or over a long interruption. Mark them clearly as checkpoints. They are never the accepted commit; after PASS, the Orchestrator packages the verified workspace as the accepted commit (fold checkpoints in per project rules). The diff under verification is still measured from the baseline.
- Push and merge need Human authorization unless the project delegates them.

## Workflow

### 1. Start

The Orchestrator:

1. Takes the first Task in `list_ready`, unless a dependency or explicit instruction says otherwise.
2. Reads the Task with `get_task`, plus its facts, and checks [readiness](#task-definition-and-readiness). If it is not ready, it does not start it; it reports to the Human (see [Stop for decision](#stop-for-decision)).
3. Records `IN_PROGRESS` with `set_task_state`.
4. Prepares a clean workspace (worktree/branch per project rules) and notes its baseline commit. It launches a Worker with the Task's full content, any prior facts, the workspace location, and the baseline. It records an `execution` fact, including the baseline.

### 2. Development and delivery

The Worker works only within the Task definition and keeps its own plan internal. When done, it self-checks, leaves the workspace stable and uncommitted, and computes the delivery fingerprint. It then records a `delivery` fact and returns the same summary to the Orchestrator.

If the Worker hits a question it cannot settle within the Task definition, or the Task looks wrong, it stops that part and reports. It does not widen the scope, rewrite acceptance criteria, or set state.

### 3. Independent verification

Before launching a Verifier, the Orchestrator confirms:

- a `delivery` fact exists for the current workspace;
- the Worker has finished self-checking and is no longer writing;
- no other writer can touch the workspace;
- the workspace still matches the delivered fingerprint.

The Orchestrator then launches a **new** Independent Verifier with the Task, its acceptance criteria, the complete diff from the baseline, and the actual workspace. It records an `execution` fact with the available provenance. The Verifier forms its own verification from those inputs. Worker delivery notes are claims and evidence to check against the workspace, not the preset frame for verification.

The Verifier:

- confirms the fingerprint at the start and again at the end. If it changed, it stops, reports it, and its conclusion is void;
- derives verification scope from the Task's acceptance boundary and plausible impact radius, and checks the minimum evidence needed for correctness and safety. Repository-wide build, test, vet, or e2e checks are not default actions. It may expand validation for a specific risk found in the delivery, broader plausible impact, uncertainty in available evidence, or an explicit Task requirement; it gives the reason for that expansion rather than expanding merely to be safe. Unrelated, slow, or failing checks do not automatically become delivery gates;
- checks each acceptance criterion against the full diff from the baseline and the related code, not only the Worker's summary;
- follows the real business path through code and tests. It judges whether mocks, hand-built data, or same-source assumptions bypass the core risk. It runs checks itself when the evidence is not enough, including real API/UI/database checks when they apply;
- looks for missed edge cases, errors, logging, secrets, and changes outside the Task's scope;
- may read earlier verification facts, but verifies the whole Task again rather than only the previous RC items;
- returns a verdict with its evidence and limitations, and records it as a `verification` fact.

Passing tests do not by themselves mean PASS. Any relevant check that failed or was skipped is named, its impact assessed, and judged against the Task.

### 4. Verifier PASS → DONE

When a Verifier returns PASS:

1. The Orchestrator checks that the PASS names the fingerprint still in the workspace.
2. Before packaging, the Orchestrator confirms the workspace still matches the PASS baseline and fingerprint. If it does not, the commit is not accepted and the Task does not become DONE.
3. The Orchestrator packages that unchanged workspace as the Task's accepted implementation commit. It then confirms two things: the commit's diff from the baseline matches the PASS fingerprint, and the workspace is clean. If either check fails, the commit is not accepted and the Task does not become DONE. Any content change is a new delivery: the Worker makes it, then a new Independent Verifier verifies it before it can be accepted.
4. The Orchestrator records a `delivery` fact naming the accepted commit, then records `DONE` with a short reason such as `Accepted <sha>`.

The `verification` fact is the completion evidence; the accepted commit ties it to Git. Packaging the commit does not give the Orchestrator implementation-writing or verification responsibility.

`PASS` is exclusively an Independent Verifier verdict. A Human decision must never be recorded or described as `PASS`.

### 5. Human acceptance

A Human may explicitly accept a Task without an Independent Verifier PASS. The Human records that choice as a `decision` fact (for example `decision: accept` with the reason), and the Human or Orchestrator may then record `DONE`. The decision fact is the completion evidence for this path; do not create a `verification` fact for the Human decision. A `decision` fact by itself does not change Task state.

The Worker's word, a self-review, or the Orchestrator's own reading do not justify DONE.

### 6. RC

An RC verdict means the delivery does not meet the Task, but it can be fixed within the current definition.

1. The Task stays `IN_PROGRESS`. Nothing changes on the Board except the `verification` fact.
2. The Orchestrator gives the RC issues and evidence to the current Worker, or to a replacement (see [Interruption and takeover](#interruption-and-takeover)).
3. The Worker corrects in the same uncommitted workspace, self-checks, and records a new `delivery` fact with the new fingerprint.
4. The Orchestrator launches a **new** Independent Verifier. No conclusion from before the correction carries over.

If the rounds stop converging, stop for decision. Signs of this: the same issue keeps returning, the Worker and Verifier disagree about what the criteria require, or a fix would need a definition change. The Orchestrator does not overrule a Verifier, and it does not record DONE over an open RC.

### 7. BLOCKED and resume

A Verifier's `BLOCKED` verdict, or a Worker report that it cannot continue, is input to the Orchestrator. It is not a state change.

The Orchestrator first tries to resolve the issue within the run, for example by fixing the environment or getting a quick answer from a Human who is present. When the Task cannot continue until something outside the current run happens, the Orchestrator:

1. makes sure no agent is still writing to the workspace;
2. records a `handoff` fact with the confirmed facts, current workspace state, and exactly what is needed;
3. records `BLOCKED` with a one-line reason naming the blocker or the question.

To resume, once the blocker is resolved and any Human decision is recorded as a `decision` fact, an Orchestrator records `BLOCKED → IN_PROGRESS` (clearing the reason). It continues from the existing workspace and facts as in a takeover. BLOCKED is never a completion verdict.

### Stop for decision

Stop the affected work and ask the Human when:

- the Task's goal, scope, dependencies, or acceptance criteria would have to change, including splitting, merging, or re-cutting it;
- the criteria are ambiguous, contradictory, or cannot be verified;
- a product or architecture question arises, or existing code or docs conflict with `PRODUCT.md`, `ARCHITECTURE.md`, or this Skill;
- an action is outward-facing or irreversible and not delegated: push, merge, release, deploy, external messages, deleting data or history;
- RC rounds are not converging, or roles disagree about what the Task requires;
- the execution backend for the run is unspecified;
- secrets, security, or legal/licensing concerns appear.

If the Human answers within the run, record the decision as a `decision` fact; if the definition changed, the Human or Orchestrator records it with `update_task`. The Task stays `IN_PROGRESS` unless the decision explicitly accepts it. If the answer will not come within the run, record `BLOCKED` as in section 7. There is no separate decision state.

### Interruption and takeover

Continuing does **not** require the same agent instance. A Worker, Verifier, or Orchestrator that stops, times out, loses contact, or whose harness exits can be replaced.

**Write authority first.** Before a replacement Worker may write, the Orchestrator confirms the predecessor can no longer write to the workspace: it stopped, was cancelled or archived, or lost access through an equivalent mechanism. A transport error, timeout, or silent session shows only that the Orchestrator cannot see it; it does not show that the predecessor stopped. Never start a parallel writer on that evidence alone.

**Replacing a Worker.** This covers a light interruption, with goal, scope, and workspace still trustworthy. The Orchestrator records an `execution` fact for the new Worker and, when useful, a `handoff` fact. The replacement reads:

- the Task;
- its facts (`delivery`, `verification`, `decision`, `handoff`) and events;
- the workspace and Git state: the uncommitted diff against the baseline, and any checkpoint commits;
- the predecessor's agent activity, where available.

It re-checks any runtime state that may have changed, then does only the remaining work. A takeover in a different workspace or on another machine starts from a checkpoint commit. That is the case checkpoints exist for. If files change that nobody expected, the replacement or Orchestrator pauses writing and finds any executor that may still be running. Writing resumes only once the workspace is stable. Nothing is verified until it is stable.

**Serious interruption.** If the goal, boundaries, dependencies, or workspace state are no longer reliable, pause. Within the existing definition the Orchestrator may roll back or restart the work. If recovery needs a definition change, stop for decision.

**Replacing a Verifier.** An interrupted verification yields no verdict. Launch a new Verifier on the stable delivery.

**Orchestrator recovery.** A new Orchestrator resumes before taking anything from READY:

1. List the `IN_PROGRESS` Tasks. For each, read its facts and events, inspect its workspace, and find out from the harness whether any agent is still running on it.
2. Continue each Task from where its records show it stands, applying the write-authority rule above.
3. Leave `BLOCKED` Tasks alone unless their blocker is known to be resolved.

A Worker that must stop with work unfinished records a `handoff` fact, or reports it if it has no Board access, before exiting when it can. If it cannot, the records above are the handoff.

### Closure

After DONE, the Orchestrator closes out the Task:

- Stops or releases the Task's Worker and Verifier sessions so nothing keeps writing.
- Integrates the accepted commit according to the project's Git rules. Push and merge need Human authorization unless the project delegates them. If integrating would change content (a non-trivial conflict resolution, say), that is new delivery: the Worker makes the change and a new Independent Verifier checks it before integration.
- Drafts follow-up work found during the Task as unqueued Tasks, or reports it to the Human. It does not stretch the finished Task.
- Reports to the Human: the Task, the accepted commit, a verification summary, unverified items and limitations, and follow-ups.

The Orchestrator then takes the next READY Task only if the current run authorizes continuing.

## Self-check for anyone changing this Skill

- No rule needs the Board to authorize a role, enforce a transition, or detect a workflow phase.
- Exactly four Task-level states; RC, verification, handoff, and decisions stay in facts.
- Nothing depends on a particular harness, model, or product.
- Internal todos are not Board Tasks.
- Worker self-checks never stand in for Independent Verification; every verification round, including after RC, uses a new Verifier.
- No required progress file, and no implementation commit before PASS.
- New rules answer observed failures, not hypothetical ones.
