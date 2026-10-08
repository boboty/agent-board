# Complete your first Task

This page walks through one complete Orchestrator → Worker → Independent Verifier → DONE cycle with the CLI. It assumes you have finished [Install with your agent](install-with-agent_EN.md) and `aboard doctor` passes. It is a concrete example of the [Workflow Skill](../workflow/SKILL.md) and [Management Skill](../management/SKILL.md), not a separate specification; if they disagree, the Skills win.

The CLI is sufficient; MCP, Paseo, Orca, or another scheduler is not required. Task writes need an actor label for traceability; that label does not grant Human authorization.

## 1. Orchestrator: define, queue with explicit authorization, and start the Task

`BOARD_DIR` is the absolute path of the initialized target repository. `ORCH_ACTOR` is this Orchestrator's writer label; session/harness/model come from the current Orchestrator session metadata. The Human defines the Task or explicitly delegates drafting. Show the complete definition and obtain explicit queue authorization before queueing. These commands assume the Human has delegated draft creation and the queue write to the current Orchestrator; otherwise, the Human runs those write commands. Set `TASK_REF` from the `number` in the `task create` JSON and `DRAFT_VERSION` from the `version` in the following `task get`. Queue only after the Human explicitly authorizes this Task. Read the Task again after queueing, and use the latest `version` for the next state change. Actor is trace metadata, not authorization.

```bash
set -euo pipefail
BOARD_DIR="<absolute path to the initialized target repository>"
ORCH_ACTOR="orchestrator/<current Orchestrator label>"
ORCH_SESSION="<session of the current Orchestrator session>"
ORCH_HARNESS="<harness of the current Orchestrator session>"
ORCH_MODEL="<model of the current Orchestrator session>"
CREATE_KEY="<unique idempotency key for this task create>"
aboard task create --title "<short outcome>" --description "<goal, scope, and boundaries>" --acceptance "<independently checkable criteria>" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "$CREATE_KEY"
```

Copy `number` from the `task create` JSON output, then read the draft version:

```bash
TASK_REF="<number from task create output>"
aboard task get "$TASK_REF" --dir "$BOARD_DIR"
# Copy the version from that output into DRAFT_VERSION.
DRAFT_VERSION="<version from the latest task get>"
```

Only after the Human explicitly authorizes queueing this Task, run the following. If the queue write was not delegated to the Orchestrator, the Human runs it and uses their own actor label (for example `human/name`) instead of `ORCH_ACTOR`:

```bash
set -euo pipefail
aboard task queue "$TASK_REF" --version "$DRAFT_VERSION" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "task-${TASK_REF}-queue-${DRAFT_VERSION}"
aboard task get "$TASK_REF" --dir "$BOARD_DIR"
```

After confirming the Task is `READY`, the Orchestrator reads its latest version and starts it. A Human who queues the Task does not perform the Orchestrator's state transition:

```bash
set -euo pipefail
aboard task get "$TASK_REF" --dir "$BOARD_DIR"
# Confirm state is READY and copy the latest version into READY_VERSION.
READY_VERSION="<version from the latest task get>"
aboard task set-state "$TASK_REF" IN_PROGRESS --version "$READY_VERSION" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "task-${TASK_REF}-start-${READY_VERSION}"
aboard task get "$TASK_REF" --dir "$BOARD_DIR"
```

After preparing and launching the Worker's workspace, get `WORKTREE` from the Orchestrator's actual launch result. Get `BRANCH` and `BASELINE` from Git in that workspace. Get the Worker session, harness, and model from its launch/profile metadata:

```bash
WORKTREE="<absolute path of the workspace actually assigned to Worker>"
BRANCH=$(git -C "$WORKTREE" branch --show-current)
BASELINE=$(git -C "$WORKTREE" rev-parse HEAD)
WORKER_SESSION="<session from the Worker launch result>"
WORKER_HARNESS="<harness from the Worker launch result>"
WORKER_MODEL="<model from the Worker launch result>"
aboard fact record "$TASK_REF" --kind execution --body "Started Worker in $WORKTREE on $BRANCH at $BASELINE." --data "{\"worktree\":\"$WORKTREE\",\"branch\":\"$BRANCH\",\"baseline\":\"$BASELINE\"}" --role worker --session "$WORKER_SESSION" --harness "$WORKER_HARNESS" --model "$WORKER_MODEL" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "task-${TASK_REF}-worker-execution-${WORKER_SESSION}"
```

The Orchestrator records this execution fact after launching the Worker. `role` describes the launched Worker; `--actor` identifies the Orchestrator that wrote the fact. `IN_PROGRESS` is the start boundary. Queue authorization does not authorize push, merge, or release.

## 2. Worker: execute and deliver evidence

Give the IN_PROGRESS Task, acceptance criteria, `TASK_REF`, `BOARD_DIR`, `WORKTREE`, `BRANCH`, `BASELINE`, and relevant checks to the Worker. Shell variables do not transfer between sessions; the Worker reassigns them in its session from the launch handoff. `WORKER_SESSION`, `WORKER_HARNESS`, and `WORKER_MODEL` come from the actual Worker session metadata. The Worker computes the fingerprint after its changes and self-checks, once the workspace is stable. This read-only Python function hashes each changed path, Git mode, and Git blob ID; worktree calculation combines tracked changes and untracked files from `git ls-files --others --exclude-standard`. For accepted-commit comparison it reads that commit's tree and uses the same manifest, so a formerly untracked file has the same fingerprint after commit. `git hash-object` is used without `-w`, so it does not write Git objects. The Orchestrator, Worker, and Verifier must each define this exact same `fingerprint()` function in their own shell. In every handoff, provide this guide path, `docs/first-task_EN.md`, and copy the complete function code block; shell functions do not transfer between sessions.

```text
Execute only this IN_PROGRESS Agent Board Task, following agent-board-workflow. When finished, leave the workspace stable and uncommitted. Record the relevant check commands and raw results, unverified items and limitations, then report them to the Orchestrator. Do not claim PASS, commit, push, merge, or release.
```

```bash
set -euo pipefail
fingerprint() {
  python3 - "$1" "${2:-}" "$WORKTREE" <<'PY'
import hashlib, os, stat, subprocess, sys
base, commit, root = sys.argv[1], sys.argv[2], sys.argv[3]
def git(*args, input=None):
    return subprocess.check_output(["git", "-C", root, *args], input=input)
def splitz(raw):
    return [part for part in raw.split(b"\0") if part]
args = ["diff", "--name-only", "-z", "--no-renames", base]
if commit:
    args.append(commit)
paths = set(splitz(git(*args, "--")))
tree = {}
if commit:
    for row in splitz(git("ls-tree", "-rz", "--full-tree", commit)):
        meta, path = row.split(b"\t", 1)
        mode, kind, oid = meta.split(b" ", 2)
        if kind == b"blob": tree[path] = (mode, oid)
else:
    paths.update(splitz(git("ls-files", "--others", "--exclude-standard", "-z")))
h = hashlib.sha256()
h.update(b"agent-board-delivery-fingerprint-v1\0" + base.encode() + b"\0")
for path in sorted(paths):
    if commit:
        entry = tree.get(path)
    else:
        name = os.path.join(root, os.fsdecode(path))
        try: info = os.lstat(name)
        except FileNotFoundError: info = None
        if info is None: entry = None
        elif stat.S_ISLNK(info.st_mode):
            entry = (b"120000", git("hash-object", "--path=" + os.fsdecode(path), "--stdin", input=os.fsencode(os.readlink(name))).strip())
        elif stat.S_ISREG(info.st_mode):
            mode = b"100755" if info.st_mode & 0o111 else b"100644"
            entry = (mode, git("hash-object", "--path=" + os.fsdecode(path), "--", name).strip())
        else: raise SystemExit("unsupported changed path type: " + repr(name))
    mode, oid = entry if entry else (b"deleted", b"")
    h.update(len(path).to_bytes(8, "big") + path + mode + b"\0" + oid + b"\0")
print(h.hexdigest())
PY
}
FINGERPRINT=$(fingerprint "$BASELINE" "" "$WORKTREE")
printf '%s\n' "$FINGERPRINT"
```

The Worker records the delivery fact with structured baseline/fingerprint, branch/worktree and actual check results in `data`, and provenance from that Worker session. `--actor` is the Worker label because the Worker writes this fact:

```bash
WORKER_ACTOR="worker/<trace label for this Worker>"
aboard fact record "$TASK_REF" --kind delivery --body "<changes, actual check results, unverified items, and limitations>. Fingerprint method: agent-board-delivery-fingerprint-v1, SHA-256 of sorted changed path + Git mode + blob ID, including tracked and nonignored untracked files; recompute with the function in this guide and pass the accepted commit to compare." --baseline "$BASELINE" --fingerprint "$FINGERPRINT" --data "{\"branch\":\"$BRANCH\",\"worktree\":\"$WORKTREE\",\"checks\":\"<commands and raw results>\"}" --role worker --session "$WORKER_SESSION" --harness "$WORKER_HARNESS" --model "$WORKER_MODEL" --dir "$BOARD_DIR" --actor "$WORKER_ACTOR" --key "task-${TASK_REF}-delivery-${FINGERPRINT}"
```

Self-check is not a PASS; a delivery fact records delivery, not a verification verdict.

## 3. A new Independent Verifier: independent, read-only acceptance

After confirming the Worker has stopped writing and a recomputed fingerprint matches its delivery fact, the Orchestrator starts a new session distinct from both Worker and Orchestrator. The launch prompt includes `TASK_REF`, `BOARD_DIR`, `WORKTREE`, `BRANCH`, `BASELINE`, the delivery fact's `FINGERPRINT`, this guide path `docs/first-task_EN.md`, and the complete fingerprint function below; shell variables and functions do not transfer between sessions, so the Verifier reassigns them and copies the identical function into its session. Record an execution fact for every newly launched Verifier, including after RC; obtain session/harness/model from that new session's launch result. Do not ask the Worker to write a verdict:

```text
You are the new Independent Verifier for this Task. Read-only inspect the Task definition and full acceptance criteria, the complete diff from baseline including untracked files, and the Worker delivery fingerprint. Recompute it with the fingerprint function the Orchestrator includes with this prompt, at the beginning and end. Run relevant checks yourself and preserve raw evidence. Do not change the workspace, Task, Skills, or docs and do not commit. Report evidence and limitations for each criterion; only you decide and record PASS, RC, or BLOCKED from evidence. Do not assume PASS from the Worker's self-check. In your shell, define the complete `fingerprint()` function attached from `docs/first-task_EN.md`.
```

After each newly launched Verifier session, the Orchestrator records its execution fact. Copy the first six context values from `task get` and the Worker facts; obtain the three `VERIFIER_*` provenance values from this new session's launch result:

```bash
TASK_REF="<number from task get>"
BOARD_DIR="<absolute path to the initialized target repository>"
WORKTREE="<worktree from the Worker execution/delivery fact>"
BRANCH="<branch from the Worker execution/delivery fact>"
BASELINE="<baseline from the Worker execution/delivery fact>"
FINGERPRINT="<fingerprint from the Worker delivery fact>"
VERIFIER_SESSION="<session from this new Verifier launch>"
VERIFIER_HARNESS="<harness from this new Verifier launch>"
VERIFIER_MODEL="<model from this new Verifier launch>"
aboard fact record "$TASK_REF" --kind execution --body "Started Independent Verifier in $WORKTREE on $BRANCH for delivery $FINGERPRINT." --data "{\"worktree\":\"$WORKTREE\",\"branch\":\"$BRANCH\",\"baseline\":\"$BASELINE\",\"fingerprint\":\"$FINGERPRINT\"}" --role verifier --session "$VERIFIER_SESSION" --harness "$VERIFIER_HARNESS" --model "$VERIFIER_MODEL" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "task-${TASK_REF}-verifier-execution-${VERIFIER_SESSION}"
```

Once it has reached its own verdict, that Verifier session records the verification fact. The structured fields carry verdict, baseline, and fingerprint; provenance comes from the current Verifier session:

```bash
VERIFIER_ACTOR="verifier/<trace label for this Verifier>"
VERDICT="<this Verifier's evidence-based PASS, RC, or BLOCKED>"
aboard fact record "$TASK_REF" --kind verification --verdict "$VERDICT" --baseline "$BASELINE" --fingerprint "$FINGERPRINT" --body "<evidence and limitations for every criterion; for RC include issue and evidence; for BLOCKED state the blocker>" --role verifier --session "$VERIFIER_SESSION" --harness "$VERIFIER_HARNESS" --model "$VERIFIER_MODEL" --dir "$BOARD_DIR" --actor "$VERIFIER_ACTOR" --key "task-${TASK_REF}-verification-${VERIFIER_SESSION}"
```

`RC` returns to the same Task for correction and a new Verifier checks the new delivery; `BLOCKED` names the blocker. A verdict does not automatically change Task state.

## 4. After PASS: Orchestrator packages the same delivery and records DONE

Only after an Independent Verifier has recorded `PASS` with the same `BASELINE` and `FINGERPRINT` may the Orchestrator continue. Read the PASS fact and put its fingerprint into `FINGERPRINT`; recompute the worktree and compare before packaging. Then package only this unchanged worktree. Read `ACCEPTED_COMMIT` from Git, recompute against that commit, require an exact match, and require a clean worktree:

```bash
set -euo pipefail
WORKSPACE_FINGERPRINT=$(fingerprint "$BASELINE" "" "$WORKTREE")
test "$WORKSPACE_FINGERPRINT" = "$FINGERPRINT"
git -C "$WORKTREE" add -A
git -C "$WORKTREE" commit -m "Task $TASK_REF accepted delivery"
ACCEPTED_COMMIT=$(git -C "$WORKTREE" rev-parse HEAD)
COMMIT_FINGERPRINT=$(fingerprint "$BASELINE" "$ACCEPTED_COMMIT" "$WORKTREE")
test "$COMMIT_FINGERPRINT" = "$FINGERPRINT"
test -z "$(git -C "$WORKTREE" status --porcelain)"
```

Next record the accepted delivery fact (structured baseline, fingerprint, accepted commit; Orchestrator provenance), then read the latest Task version and set DONE:

```bash
aboard fact record "$TASK_REF" --kind delivery --body "Accepted commit $ACCEPTED_COMMIT contains the unchanged PASS-verified delivery; fingerprint comparison matched and the worktree is clean." --baseline "$BASELINE" --fingerprint "$FINGERPRINT" --accepted-commit "$ACCEPTED_COMMIT" --data "{\"branch\":\"$BRANCH\",\"worktree\":\"$WORKTREE\"}" --role orchestrator --session "$ORCH_SESSION" --harness "$ORCH_HARNESS" --model "$ORCH_MODEL" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "task-${TASK_REF}-accepted-delivery"
aboard task get "$TASK_REF" --dir "$BOARD_DIR"
# Copy the version from this latest output into DONE_VERSION.
DONE_VERSION="<version from the latest task get>"
aboard task set-state "$TASK_REF" DONE --version "$DONE_VERSION" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "task-${TASK_REF}-done-${DONE_VERSION}"
aboard task get "$TASK_REF" --dir "$BOARD_DIR"
```

If fingerprints differ or the worktree is not clean, stop; do not record the accepted delivery or DONE. The Worker must make a new delivery and it must be verified again. The `git commit` above is only for the Orchestrator after PASS. Installation or creating/queueing a Task does not authorize execution, commit, push, merge, or release. Push, merge, and release still require explicit Human authorization for that run.

The [Workflow Skill](../workflow/SKILL.md) and [Management Skill](../management/SKILL.md) define the full operation contract, fact structure, and exception handling.
