# 使用 Agent 安装 Agent Board

将 README 中的提示交给 Codex、Claude Code 或 OpenCode。Agent 应依据当前系统和实际可用条件选择安装路径；不要把无法验证的步骤算作成功。

## 可复制的安装指令

```text
请按照 https://github.com/boboty/agent-board/blob/main/docs/install-with-agent.md 为当前环境安装并验证 Agent Board。

1. 先确认当前操作系统和架构、当前会话使用的 Harness，以及我指定的目标 Git 仓库。Harness 以当前会话/应用为准；只确认当前环境，不扫描其他 Harness 或无关仓库。目标仓库不明确时，先问我路径，不要猜测。
2. 检查 `aboard` 是否已安装且能运行。若没有，依据系统和架构选择 Agent Board Releases 中可用的预编译版本；如果本机已有 Go 1.25 或更新版本，也可使用 `go install github.com/boboty/agent-board/cmd/aboard@latest`。将 binary 放入已有 PATH 目录；如需调整 PATH，只改当前用户所用的必要配置，并报告所改路径。若两种路径都不可用，停止并说明缺少什么以及下一步。
3. 运行 `aboard version`，确认实际执行的 binary 路径和版本（可用 `command -v aboard`；Windows 使用当前 Shell 的等效命令）。如果新开的 Shell 才能识别 PATH，说明需要重新打开 Shell；未验证前不要报告 CLI 安装完成。
4. 运行 `aboard skill install` 和 `aboard skill check`。安装会把 `agent-board-management` 与 `agent-board-workflow` 放到当前支持的用户级目录：Codex 使用 `~/.agents/skills/`；Claude Code 与 OpenCode 共用 `~/.claude/skills/`。默认安装会补缺失项并保留内容不同的文件。对不同内容先报告其路径和 `different` 状态，不要加 `--force`；只有我明确授权覆盖对应 Skill 后，才运行 `aboard skill install --force management` 或 `aboard skill install --force workflow`，然后再次检查。
5. 在我指定的 Git 仓库目录运行 `aboard init`，再运行 `aboard doctor`。`init` 会在该仓库的 Git common directory 保存本机项目身份，并在用户数据目录创建 Board 数据；这些不是仓库工作树中的文件。若目标不是 Git 仓库、无法写入，或 doctor 失败，停止并报告精确原因和修复所需的下一步；不要清理或覆盖已有项目数据。
6. 最后逐项报告：已确认的系统/架构和当前 Harness；aboard 版本与 binary 路径；两个 Skills 在各支持目录的 `current`、`missing` 或 `different` 状态；目标仓库路径及 `init` 结果；`doctor` 的完整结果；未解决问题及下一步。只有上述必要检查都成功且目标项目已初始化，才称“安装并就绪”。安装不代表已定义/入队/执行 Task，也不授权 commit、push、merge 或 release。

首次使用请阅读同页“首次跑通一个 Task”。MCP 是可选入口；这条路径使用 CLI，不需要 MCP、Paseo、Orca 或其他调度产品。
```

## Agent 应如何判断安装结果

`aboard doctor` 会检查 binary、Skills 和当前项目配置。它以退出码 `0` 表示没有发现问题，退出码 `1` 表示有组件需要处理，退出码 `2` 表示命令用法错误。doctor 的 Skills 状态同时列出 Claude Code、Codex 和 OpenCode 的安装位置；当前实现中 Claude Code 与 OpenCode 指向同一份 `~/.claude/skills` 文件。doctor 是本地诊断，不会安装缺少的组件。

当前 Release 说明的平台为 macOS（Apple Silicon、Intel）、Linux（amd64、arm64）和 Windows（amd64）。若 Release 没有当前系统/架构的 binary，可在 Go 1.25+ 环境选择 Go 安装；否则报告未支持的组合及下一步，不要声称已安装。Go 安装后仍须实际确认 `aboard` 在当前 Shell 的 PATH 中。

Harness 路径依据当前 CLI 的 Skills 目标：Codex 使用 `~/.agents/skills/agent-board-<name>/SKILL.md`；Claude Code 与 OpenCode 共用 `~/.claude/skills/agent-board-<name>/SKILL.md`。`aboard skill check` 和 `doctor` 只检查这些文件的内容，不会启动 Harness 来确认它已加载 Skill。本指南没有实测三个 Harness 的实际发现/加载行为；若当前会话看不到 Skill，应报告“文件已安装、Harness 加载未验证”，并提示用户重新打开 Harness 或检查其 Skill 发现说明。

遇到旧版或不同来源的 Skill 时，`aboard skill check` 会显示状态和路径。普通 `skill install` 保留不同内容；`--force` 是覆盖授权，不是普通修复选项。参见[运维说明](operations.md)。

## 首次跑通一个 Task

这是可直接采用的角色提示。使用 CLI 即可；Task 写操作需要明确 actor 标签，但该标签只用于追溯，不代替 Human 授权。

### 1. Orchestrator：定义、获授权入队并启动 Task

`BOARD_DIR` 是已初始化的目标仓库绝对路径；`ORCH_ACTOR` 是本 Orchestrator 的写入标签；session/harness/model 来自当前 Orchestrator 会话信息。Task 定义由 Human 提供，或由 Human 明确委托的代理起草；入队前展示完整定义并取得明确授权。下面命令假设 Human 已将创建草稿和 queue 写操作委托给当前 Orchestrator；若没有委托，Human 自己运行对应写命令。`TASK_REF` 取 `task create` JSON 的 `number`，`DRAFT_VERSION` 取紧接着 `task get` 的 `version`。只有 Human 明确授权此 Task 入队后，才运行 queue；其后的状态变化版本必须重新从最新 `task get` 读取。Actor 用于追溯，不代表授权。

```bash
set -euo pipefail
BOARD_DIR="<已初始化目标仓库的绝对路径>"
ORCH_ACTOR="orchestrator/<当前 Orchestrator 标签>"
ORCH_SESSION="<当前 Orchestrator 会话的 session>"
ORCH_HARNESS="<当前 Orchestrator 会话的 harness>"
ORCH_MODEL="<当前 Orchestrator 会话的 model>"
CREATE_KEY="<本次唯一的 task-create 幂等键>"
aboard task create --title "<简短目标>" --description "<目标、范围与边界>" --acceptance "<独立可检查的验收条件>" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "$CREATE_KEY"
```

从 `task create` JSON 输出复制 `number`，然后读取草稿版本：

```bash
TASK_REF="<task create 输出的 number>"
aboard task get "$TASK_REF" --dir "$BOARD_DIR"
# 将上面返回的 version 原样填入 DRAFT_VERSION。
DRAFT_VERSION="<刚才 task get 的 version>"
```

Human 明确授权后才执行。若未委托给 Orchestrator，由 Human 自己运行这些写命令，并把 `--actor` 换成 Human 自己的实际标签（例如 `human/name`）：

```bash
set -euo pipefail
aboard task queue "$TASK_REF" --version "$DRAFT_VERSION" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "task-${TASK_REF}-queue-${DRAFT_VERSION}"
aboard task get "$TASK_REF" --dir "$BOARD_DIR"
```

确认返回的 Task 为 `READY` 后，Orchestrator 重新读取最新版本并启动；Human 直接 queue 时不代替 Orchestrator 执行状态转换：

```bash
set -euo pipefail
aboard task get "$TASK_REF" --dir "$BOARD_DIR"
# 从最新输出确认 state 为 READY，并将 version 复制到 READY_VERSION。
READY_VERSION="<上次 task get 的 version>"
aboard task set-state "$TASK_REF" IN_PROGRESS --version "$READY_VERSION" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "task-${TASK_REF}-start-${READY_VERSION}"
aboard task get "$TASK_REF" --dir "$BOARD_DIR"
```

Orchestrator 准备并启动 Worker 的工作区后，从启动结果取得 `WORKTREE` 绝对路径及 Worker 会话的 `session`、`harness`、`model`（例如 Paseo Agent 详情）。在工作区读取 branch 和 baseline：

```bash
WORKTREE="<Orchestrator 实际交给 Worker 的工作区绝对路径>"
BRANCH=$(git -C "$WORKTREE" branch --show-current)
BASELINE=$(git -C "$WORKTREE" rev-parse HEAD)
WORKER_SESSION="<Worker 启动结果中的 session>"
WORKER_HARNESS="<Worker 启动结果中的 harness>"
WORKER_MODEL="<Worker 启动结果中的 model>"
aboard fact record "$TASK_REF" --kind execution --body "Started Worker in $WORKTREE on $BRANCH at $BASELINE." --data "{\"worktree\":\"$WORKTREE\",\"branch\":\"$BRANCH\",\"baseline\":\"$BASELINE\"}" --role worker --session "$WORKER_SESSION" --harness "$WORKER_HARNESS" --model "$WORKER_MODEL" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "task-${TASK_REF}-worker-execution-${WORKER_SESSION}"
```

这个 execution fact 由 Orchestrator 在启动 Worker 后写入。执行者在 fact provenance 中仍标为 `worker`，而 `--actor` 标识实际写入它的 Orchestrator。Task 进入 `IN_PROGRESS` 才可开工。入队授权不包括 push、merge 或 release。

### 2. Worker：执行并交付证据

将 Task、验收标准、`TASK_REF`、`BOARD_DIR`、`WORKTREE`、`BRANCH`、`BASELINE` 和可用检查交给 Worker。shell 变量不会跨会话传递，Worker 在自己的会话中按启动信息重新赋值；`WORKER_SESSION`、`WORKER_HARNESS`、`WORKER_MODEL` 取当前 Worker 会话的实际信息。Worker 在完成改动和自检、工作区稳定后计算 fingerprint。下面的只读 Python 函数对变化文件的路径、Git mode 和 Git blob ID 做 SHA-256；worktree 计算会合并已跟踪差异与 `git ls-files --others --exclude-standard` 的未跟踪文件。比较 accepted commit 时读取该 commit 的 tree，使用同一 manifest，因此新文件提交后会得到相同 fingerprint。`git hash-object` 不带 `-w`，不写 Git 对象。Orchestrator、Worker、Verifier 必须分别在各自 shell 中定义本节完全相同的 `fingerprint()` 函数；交接时提供本指南路径 `docs/install-with-agent.md` 并把整个函数代码块一并复制给接收会话。shell 函数不会跨会话传递。

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

Worker 记录 delivery fact：结构化 `--baseline`、`--fingerprint` 必须填写；`data` 记录 branch、worktree 和实际检查结果；provenance 使用本 Worker 的启动信息。`--actor` 是实际写 fact 的 Worker 标签。

```text
只执行 Agent Board 中这个 IN_PROGRESS Task，遵循 agent-board-workflow。完成后保持工作区稳定且未提交，记录相关检查命令及原始结果、未验证项和限制，再把这些信息交给 Orchestrator。不要自称 PASS，不要提交、push、merge 或 release。
```

```bash
WORKER_ACTOR="worker/<本 Worker 的追溯标签>"
aboard fact record "$TASK_REF" --kind delivery --body "<变更、实际检查结果、未验证项和限制>. Fingerprint method: agent-board-delivery-fingerprint-v1, SHA-256 of sorted changed path + Git mode + blob ID, including tracked and nonignored untracked files; recompute with the function in this guide and pass the accepted commit to compare." --baseline "$BASELINE" --fingerprint "$FINGERPRINT" --data "{\"branch\":\"$BRANCH\",\"worktree\":\"$WORKTREE\",\"checks\":\"<命令及原始结果>\"}" --role worker --session "$WORKER_SESSION" --harness "$WORKER_HARNESS" --model "$WORKER_MODEL" --dir "$BOARD_DIR" --actor "$WORKER_ACTOR" --key "task-${TASK_REF}-delivery-$FINGERPRINT"
```

Worker 的自检不等于 PASS；delivery fact 记录的是交付，不是验收 verdict。

### 3. 新的 Independent Verifier：独立只读验收

Orchestrator 确认 Worker 已停止写入且重算 fingerprint 与 delivery fact 相同后，启动一个全新、与 Worker 和 Orchestrator 都不同的会话。启动提示要包含 `TASK_REF`、`BOARD_DIR`、`WORKTREE`、`BRANCH`、`BASELINE`、delivery fact 的 `FINGERPRINT`，本指南路径 `docs/install-with-agent.md`，以及下面的 fingerprint 函数完整代码块；shell 变量和函数不会跨会话传递，Verifier 在自己的会话中重新赋值并复制完全相同的函数。每次新启动 Verifier（包括 RC 后的复验）都记录 execution fact；session、harness、model 取自这次新会话的启动结果。不要让 Worker 代写 verdict。

```text
你是此 Task 的新 Independent Verifier。只读检查 Task 定义、完整验收标准、基线到当前工作区的全部 diff（包括未跟踪文件）和 Worker 的 delivery fingerprint；使用 Orchestrator 随本提示提供的 fingerprint 函数复算，并在结论前再次复算。自行运行相关检查并保留原始证据。不要修改工作区、Task、Skill 或文档，不要提交。按每条验收标准给出证据和限制，只由你依据证据决定并记录 PASS、RC 或 BLOCKED；不要因 Worker 自检通过而预设 PASS。在你的 shell 中重新定义 `docs/install-with-agent.md` 所附的完整 `fingerprint()` 函数。
```

每个新 Verifier 会话启动后，由 Orchestrator 写 execution fact。前六个变量从 `task get` 和 Worker facts 获取，`VERIFIER_SESSION`、`VERIFIER_HARNESS`、`VERIFIER_MODEL` 来自这次新会话启动结果：

```bash
TASK_REF="<task get 返回的 number>"
BOARD_DIR="<已初始化目标仓库的绝对路径>"
WORKTREE="<Worker execution/delivery fact 中的 worktree>"
BRANCH="<Worker execution/delivery fact 中的 branch>"
BASELINE="<Worker execution/delivery fact 中的 baseline>"
FINGERPRINT="<Worker delivery fact 中的 fingerprint>"
VERIFIER_SESSION="<本次新 Verifier 启动结果中的 session>"
VERIFIER_HARNESS="<本次新 Verifier 启动结果中的 harness>"
VERIFIER_MODEL="<本次新 Verifier 启动结果中的 model>"
aboard fact record "$TASK_REF" --kind execution --body "Started Independent Verifier in $WORKTREE on $BRANCH for delivery $FINGERPRINT." --data "{\"worktree\":\"$WORKTREE\",\"branch\":\"$BRANCH\",\"baseline\":\"$BASELINE\",\"fingerprint\":\"$FINGERPRINT\"}" --role verifier --session "$VERIFIER_SESSION" --harness "$VERIFIER_HARNESS" --model "$VERIFIER_MODEL" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "task-${TASK_REF}-verifier-execution-${VERIFIER_SESSION}"
```

Verifier 自行确定 `VERDICT` 为 `PASS`、`RC` 或 `BLOCKED` 后，由这个 Verifier 会话记录 verification fact。其结构化字段必须包含 verdict、baseline、fingerprint，provenance 必须来自当前 Verifier 会话：

```bash
VERIFIER_ACTOR="verifier/<本 Verifier 的追溯标签>"
VERDICT="<本 Verifier 实际得出的 PASS、RC 或 BLOCKED>"
aboard fact record "$TASK_REF" --kind verification --verdict "$VERDICT" --baseline "$BASELINE" --fingerprint "$FINGERPRINT" --body "<逐条验收证据、原始检查结果和限制；RC 写明问题及证据；BLOCKED 写明阻塞>" --role verifier --session "$VERIFIER_SESSION" --harness "$VERIFIER_HARNESS" --model "$VERIFIER_MODEL" --dir "$BOARD_DIR" --actor "$VERIFIER_ACTOR" --key "task-${TASK_REF}-verification-${VERIFIER_SESSION}"
```

`RC` 回到同一 Task 修正，之后由新 Verifier 检查新 delivery；`BLOCKED` 说明具体阻塞。Verifier 的 verdict 本身不会自动改变 Task 状态。

### 4. 仅在 PASS 后：Orchestrator 封装同一交付并记录 DONE

只有 Independent Verifier 已记录同一 `BASELINE` 和 `FINGERPRINT` 的 `PASS`，Orchestrator 才执行以下步骤。先读 Board 上的 PASS fact，把其 fingerprint 赋给 `FINGERPRINT`；复算当前工作区并比较。然后只封装这个未变工作区。`ACCEPTED_COMMIT` 从提交结果读取；再次用同一函数对比 accepted commit，确认 fingerprint 完全一致并确认工作区 clean：

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

随后 Orchestrator 先记录 accepted delivery fact（结构化 baseline、fingerprint、accepted commit；provenance 为 Orchestrator），再读取最新 Task version，最后才设置 DONE：

```bash
aboard fact record "$TASK_REF" --kind delivery --body "Accepted commit $ACCEPTED_COMMIT contains the unchanged PASS-verified delivery; fingerprint comparison matched and the worktree is clean." --baseline "$BASELINE" --fingerprint "$FINGERPRINT" --accepted-commit "$ACCEPTED_COMMIT" --data "{\"branch\":\"$BRANCH\",\"worktree\":\"$WORKTREE\"}" --role orchestrator --session "$ORCH_SESSION" --harness "$ORCH_HARNESS" --model "$ORCH_MODEL" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "task-${TASK_REF}-accepted-delivery"
aboard task get "$TASK_REF" --dir "$BOARD_DIR"
# 将上面最新输出的 version 原样填入 DONE_VERSION。
DONE_VERSION="<最后一次 task get 的 version>"
aboard task set-state "$TASK_REF" DONE --version "$DONE_VERSION" --dir "$BOARD_DIR" --actor "$ORCH_ACTOR" --key "task-${TASK_REF}-done-${DONE_VERSION}"
aboard task get "$TASK_REF" --dir "$BOARD_DIR"
```

如果任何 fingerprint 不一致或工作区不干净，停止，不记录 accepted delivery 或 DONE；由 Worker 恢复交付并重新验证。这里的 `git commit` 只在 PASS 后由 Orchestrator 执行；首次安装或创建/入队 Task 都不授权执行、commit、push、merge 或 release。push、merge、release 仍需本次运行明确的 Human 授权。

具体操作合同、事实结构与异常处理以 [Workflow Skill](../workflow/SKILL.md) 和 [Management Skill](../management/SKILL.md) 为准。
