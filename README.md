# Agent Board

[中文](README.md) | [English](README_EN.md)

**把 AI 研发任务从聊天记录里拿出来。**

如果你已经开始让 Codex、Claude Code、OpenCode 或其他 Agent 连续做多个开发任务，你大概会遇到同一个问题：

- todo 写得很好，但过一会儿就和实际进度对不上；
- 换一个会话，要重新解释“做到哪了”；
- Developer 说做完了，你还得自己追着确认有没有验收；
- 两个 Agent 一并行，很快就不知道谁在做什么；
- 真正耗人的不是写代码，而是盯执行。

Agent Board 做的事情很简单：**把 Task、状态、交付和验收放到一个共享 Board 里。**

你继续在自己习惯的 AI 对话里聊想法。想清楚了，就把它变成 Task；Task 进入 READY 后，执行器自己去做、验收、收尾。你只在需要裁决时回来。

> 用上以后，目标不是多一个看板要维护，而是少盯几个 Agent。

## 先看它怎么用

假设你正在一个项目里，发现：

> `aboard --help` 的 Skills 区域中英文风格不一致。

你不需要自己写任务卡，也不需要先打开某个专用执行界面。

### 1. 在 Codex 里说一句

> 把这个想法整理成一个 Board Task，先不要入队，也不要开始实现。

Codex 会把它整理成一个未入队 Task。你可以在 Web Board 里看一眼，觉得合适，就告诉它：

> 这个 Task 我接受了，排到 READY 末尾。

也可以直接在 Web Board 里手动入队。

### 2. 让 Paseo 开始干活

在同一个项目里启动 Paseo，告诉它：

> 从当前项目 Agent Board 读取并执行 READY Tasks，遵循 `agent-board-workflow`。使用 Paseo 已配置的 Agent Profiles。持续处理直到 READY 队列为空，或遇到需要 Human 裁决的事项时停止并报告。

之后你不用继续盯着它。

Paseo 会读取 READY Task，安排 Developer 实现，再启动一个新的 Independent Verifier 验收。PASS 后，Orchestrator 把验收过的 workspace 封装成 commit。

如果你愿意把正常收尾也交出去，再加一句：

> 如果任务完成并验收通过，自动合入 main、push origin/main，push 成功后清理对应的本地 worktree 和分支；遇到冲突或异常时停止并报告。

### 3. 你看到的是结果

```text
想法
 ↓
Task
 ↓
READY
 ↓
执行 + 独立验收
 ↓
DONE
```

平时你只需要决定：**做什么、先做什么、遇到分歧怎么裁决。**

至于由哪个 Agent 写、哪个 Agent 验、任务在哪个 worktree 里跑，这些都可以留在执行层。

## 5 分钟开始

要求：**Go 1.25+**。

安装：

```bash
go install github.com/boboty/agent-board/cmd/aboard@latest
aboard skill install
```

如果 `aboard` 不在 PATH：

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

进入你的项目：

```bash
cd your-project
aboard init
aboard doctor
aboard web
```

`aboard init` 会创建版本 2 的 `.agent-board.json`，默认以项目根目录名作为项目名称；可用 `aboard init --name "项目名称"` 指定名称。建议把它提交到仓库，这样同一项目的不同 worktree 和进程都会显示同一名称并找到同一块 Board。普通 `aboard doctor` 只读检查；旧版身份可用 `aboard doctor --fix [--name "项目名称"]` 显式迁移。

`aboard web` 会在 `127.0.0.1` 上自动选择一个空闲端口并输出 URL。多个项目可以同时开着自己的 Board。

然后就可以回到 Codex、Claude Code 或其他本地 Harness 里，用自然语言开始创建 Task。

## 日常使用是什么感觉

你可以把 Agent Board 当成 AI 研发里的共享任务账本：

```text
Codex / Claude Code / OpenCode
        ↓
   创建和管理 Task
        ↓
     Agent Board
        ↓
 Paseo / Orca / 其他 Orchestrator
        ↓
 Developer + Independent Verifier
```

管理端和执行端可以是同一个 Harness，也可以完全不同。

只有一个 Codex 也能工作；有 Paseo / Orca 这样的专用 Orchestrator 时，可以把执行进一步放到后台。不同 Task 之间足够独立时，也可以同时跑。

## 你始终保留控制权

Agent Board 不会因为 Task 存在就自动开始开发。

- Task 可以先作为草稿存在；
- 只有你接受并放入 READY，才表示它可以执行；
- 遇到产品语义不清、冲突或异常时，执行器应该停下来找 Human；
- merge / push 是否自动完成，由你在本次运行里授权。

它不是为了把 Human 从研发里拿掉，而是把 Human 从**盯过程**里拿掉。

## Board 里会看到什么

Web Board 只有四个当前状态：

- `READY` — 可以执行
- `IN_PROGRESS` — 正在执行
- `DONE` — 已完成
- `BLOCKED` — 等待处理

未入队 Task 单独显示，不会伪装成第五种状态。

每个 Task 都能看到它的内容、执行记录、交付、验收和历史事件。DONE 首页只保留最近完成的一批，完整历史可以分页查看。

## 两个 Skill 各管一件事

安装 `aboard skill install` 后，会得到两个独立 Skill：

- **`agent-board-management`**：帮你把想法整理成 Task、编辑、入队、排序；
- **`agent-board-workflow`**：告诉执行器如何协作、验收和完成交付。

你不需要记住它们的全部规则。正常情况下，在 Codex / Claude Code 等支持 Skill 的 Harness 里直接用自然语言即可。

如果需要查看：

```bash
aboard skill check
aboard skill show management
aboard skill show workflow
```

## 升级

升级 `aboard` 后，用 `doctor` 检查当前 binary、Skill 和项目状态：

```bash
go install github.com/boboty/agent-board/cmd/aboard@latest
aboard doctor
```

如果 `doctor` 报告已安装 Skill 与当前 binary 内嵌内容不同，而你要升级到内嵌版本，显式使用 `--force` 覆盖所选 Skill：

```bash
aboard skill install --force workflow
```

将 `workflow` 换成 `management` 可只升级另一个 Skill；省略 Skill 名称时，`--force` 会升级两个。普通 `aboard skill install` 会补齐缺少的 Skill，并保留内容不同的已安装版本。

## 清理当前项目

在项目目录运行 `aboard clean`，确认后会先删除该项目对应的 Board 数据，再删除项目身份文件 `.agent-board.json`：

```bash
aboard clean
```

默认会列出目标并提示 `[y/N]`；输入 `y` 或 `yes` 确认。使用 `aboard clean --yes` 可跳过确认。它只清理当前项目的身份和对应数据，不会删除 `aboard` binary、Skills 或其他项目的数据。

## 卸载

`aboard uninstall` 用于移除当前用户在这台机器上的 Agent Board：包括 `aboard` binary、Agent Board Skills，以及 `~/.agent-board/` 下的本地 Board 数据。运行前会列出目标并提示 `[y/N]`；输入 `y` 或 `yes` 确认。`--yes` 可跳过整机卸载确认：

```bash
aboard uninstall
# 跳过整体确认
aboard uninstall --yes
```

内容被改动的 Agent Board Skill 默认保留。确认要删除这些已改动的 Skill 时，可加 `--force`；它只授权删除这些 Skill，不会跳过确认：

```bash
aboard uninstall --force
```

非 Agent Board 管理的 Skill 和内容不一致的旧版遗留 Skill 始终保留，需手动处理。有保留项或删除失败时，卸载仍会尝试移除本地数据和 binary，最后以退出码 `1` 报告未完整卸载，并列出需手动处理的项目。

## CLI、Web、MCP

你可以只用 Web，也可以让 Agent 通过 CLI 或 MCP 操作同一块 Board。

常用命令：

```bash
aboard board
aboard task list
aboard ready list
aboard history 12
aboard doctor
```

MCP 是可选项，不是使用前提：

```bash
aboard mcp config claude-code
aboard mcp config codex
aboard mcp config opencode
```

## 本地优先

不需要账号，不需要部署服务，也不需要单独装数据库。

Board 数据默认保存在：

```text
~/.agent-board/<project_id>/board.db
```

仓库里只需要提交 `.agent-board.json` 这个项目身份文件。

## 它不想成为另一个“大平台”

Agent Board 不负责模型路由，不负责 Agent runtime，也不要求你换掉现有 IDE、Harness 或工作方式。

它只解决一个问题：

> **让任务管理和任务执行之间，有一个稳定、共享、可审计的交接面。**

云同步、团队服务、桌面客户端、远程执行，都可以以后由别的拼图补上。核心 Board 不需要因此变胖。

## 更多文档

- [PRODUCT.md](PRODUCT.md) — 产品定义与边界
- [ARCHITECTURE.md](ARCHITECTURE.md) — 架构与持久化
- [management/SKILL.md](management/SKILL.md) — Board Management Skill
- [workflow/SKILL.md](workflow/SKILL.md) — Workflow Skill
- [AGENTS.md](AGENTS.md) — 仓库内 Agent 开发约束

## 从源码开发

普通用户不需要 clone 仓库。参与开发时：

```bash
git clone https://github.com/boboty/agent-board.git
cd agent-board
go build -o aboard ./cmd/aboard
```

真实浏览器 e2e 位于独立 module：

```bash
cd e2e && go test ./...
```

## License

Apache-2.0。
