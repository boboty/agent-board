# Agent Board

[中文](README.md) | [English](README_EN.md)

**一个面向 AI 辅助研发的、本地优先、Harness 无关的共享任务看板。**

AI Agent 很会列 todo，但长期维护状态并不是它们的强项。任务一旦跨会话、跨 Agent、跨 Harness、跨 worktree，状态如果还留在聊天记录、todo list 或某个 Agent runtime 里，很快就会失真。

Agent Board 把**任务级共享状态**独立出来，让任务管理和任务执行通过一块稳定的 Board 解耦。

> **代码提供能力，Skill 定义规则。**

```text
Human / Codex / Claude Code / OpenCode
                 ↓
      agent-board-management
                 ↓
            Agent Board
                 ↓
       agent-board-workflow
                 ↓
      Orchestrator / Harness
          ↓             ↓
     Developer   Independent Verifier
```

管理端和执行端可以是同一个 Harness，也可以完全不同。一个 Harness 就能工作，多 Harness 和并行 Orchestrator 只是增强。

## 为什么需要 Agent Board

- **状态不住在对话里**：Task、READY 顺序、交付、验收、handoff 和审计都进入共享账本。
- **管理与执行解耦**：你可以在一个对话里讨论并排 Task，让另一个 Orchestrator 持续消费 READY。
- **Harness 无关**：CLI 是基线能力；Web 和 MCP 只是同一套 Board operations 的不同适配器。
- **本地优先**：无需账号、云服务或数据库服务器；Board 默认使用本地 SQLite。
- **适合并行协作**：不同 Task 可以在独立 worktree 中并行执行，共享同一块 Board。
- **验证成本与任务风险匹配**：Developer 和 Independent Verifier 使用最小充分证据，不默认把小改动升级成全仓体检。

## 30 秒开始

要求：**Go 1.25+**。

```bash
go install github.com/boboty/agent-board/cmd/aboard@latest
aboard skill install
```

确保 Go 的 bin 目录在 `PATH` 中：

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

在你的项目中初始化：

```bash
cd your-project
aboard init
aboard doctor
aboard web
```

`aboard init` 会创建 `.agent-board.json`。建议把它提交到仓库；它保存项目身份，让同一项目的不同 worktree 和进程自动连接到同一块 Board。

`aboard web` 默认监听 `127.0.0.1` 的一个空闲端口，并输出实际 URL，因此多个项目可以同时打开 Web Board。

## 核心模型

Agent Board 的 Task 只有四种显式状态：

- `READY`
- `IN_PROGRESS`
- `DONE`
- `BLOCKED`

Task 可以在未入队时作为草稿存在；是否进入 READY 是独立的 queue 信息，不是第五种状态。

Board 记录的是事实，包括：

- Task 定义与版本
- READY 排序
- 显式 Task 状态
- execution / delivery / verification / handoff 等 facts
- immutable audit events

Board **不**决定谁来开发、什么时候启动 Verifier、RC 怎么处理、什么时候允许 DONE。这些规则属于 Skill。

## 两个独立 Skill

### `agent-board-management`

用于任务管理：创建、编辑、接受并入队、排序和查看 Task。它不会启动实现。

典型用法是：在 Codex、Claude Code 或其他本地 Harness 中讨论想法，让 Agent 把已经想清楚的内容整理成 Board Task；Human 再决定是否入 READY。

### `agent-board-workflow`

用于任务执行：定义 Orchestrator、Developer 和 Independent Verifier 的协作边界。

核心原则包括：

- Developer 是唯一 implementation writer，负责实现与 self-check；
- Independent Verifier 使用全新、独立、只读的会话验收；
- PASS 后由 Orchestrator 将已验证 workspace 封装为 accepted commit；
- accepted commit 必须与 PASS 的 baseline / fingerprint 完全一致；
- 验证范围从 Task 验收边界和实际影响半径推导，不默认执行无关的全仓检查。

两个 Skill 相互独立，可以分别安装、检查和查看：

```bash
aboard skill install
aboard skill check
aboard skill show management
aboard skill show workflow
```

Skill 会被写入 Harness 的用户级发现路径：Claude Code / OpenCode 使用 `~/.claude/skills`，Codex 使用 `~/.agents/skills`。`aboard skill install` 也会安全处理 Agent Board 早期版本留下的受管 legacy Skill 副本。

## 一个典型工作流

```text
1. Human 在习惯的 Harness 里讨论需求
2. Management Skill 将成熟想法整理为未入队 Task
3. Human 接受后把 Task 放入 READY
4. Orchestrator 消费 READY，并创建独立 worktree
5. Developer 实现并 self-check
6. fresh Independent Verifier 只读验收
7. PASS 后 Orchestrator 封装 accepted commit
8. 经 Human 授权后可继续 merge / push / cleanup
```

如果两个 READY Task 没有依赖且改动面足够独立，可以由不同 Orchestrator 并行处理。Board 不需要为“并行”增加额外状态；Git 和 Orchestrator 负责代码集成。

## CLI、Web 与 MCP

### CLI

`aboard` 是基线入口。常用命令：

```bash
aboard board
aboard task list
aboard ready list
aboard history 12
aboard doctor
aboard operations
```

CLI 的 operation 输出为 JSON；`aboard call <operation> '<json>'` 可以直接调用和 MCP 相同的 dispatch。

### Web Board

`aboard web` 提供面向人的本地看板：

- READY / IN PROGRESS / DONE / BLOCKED 四列；
- 未入队 Task 单独显示；
- Task 详情、facts 和 audit history；
- READY 调序；
- 首页 DONE 只显示最近一批，按最近一次进入 DONE 的时间倒序；
- `/completed/` 提供完整分页历史。

默认只接受 loopback 地址。

### MCP（可选）

MCP 不是使用 Agent Board 的前提。需要结构化工具调用时，可以让 Harness 通过 stdio 访问同一套 Board operations：

```bash
aboard mcp config claude-code
aboard mcp config codex
aboard mcp config opencode
```

`mcp config` 只打印配置，不会修改 Harness 配置文件。

## 本地存储

Board 数据保存在：

```text
~/.agent-board/<project_id>/board.db
```

仓库中的 `.agent-board.json` 只保存项目身份。这样同一项目的多个 worktree、CLI、Web、MCP 和多个进程都能打开同一个 Board，而数据库本身不会进入代码仓库。

## Agent Board 明确不做什么

Agent Board 刻意保持边界克制。它不是：

- workflow engine
- Agent runtime
- model router
- 云端协作平台
- IDE / Harness 的替代品

它只把一块拼图做好：

> **为任务管理与任务执行之间提供稳定、共享、可审计的任务账本。**

外围能力可以继续组合，甚至由其他项目完成；核心 Board 不需要因此变成一个大平台。

## 项目文档

- [PRODUCT.md](PRODUCT.md) — 产品定义与边界
- [ARCHITECTURE.md](ARCHITECTURE.md) — 架构分层与持久化方向
- [management/SKILL.md](management/SKILL.md) — Board Management Skill
- [workflow/SKILL.md](workflow/SKILL.md) — Workflow Skill
- [AGENTS.md](AGENTS.md) — 仓库内 Agent 开发约束

## 从源码开发

普通用户不需要 clone 仓库。参与开发时可以：

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

Apache-2.0。选择性复用第三方项目代码时，必须保留相应 attribution 与 license notices。
