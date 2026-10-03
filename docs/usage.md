# Agent Board 日常使用

## 两个 Skill

```bash
aboard skill install
aboard skill check
aboard skill show management
aboard skill show workflow
```

- `agent-board-management`：帮助 Human 或其明确委托者定义、编辑、入队和排序 Task。
- `agent-board-workflow`：指导执行端如何协调 Worker、Independent Verifier、RC、BLOCKED 与交付。

两个 Skill 相互独立；CLI 是基础入口，MCP 是可选适配器。

## Board 状态

Agent Board 只有四个任务状态：

- `READY` — 已接受，可以执行
- `IN_PROGRESS` — 正在执行
- `DONE` — 已完成
- `BLOCKED` — 需要处理后才能继续

未入队 Task 没有生命周期状态，单独显示。

## 常用 CLI

只读：

```bash
aboard board
aboard task list
aboard ready list
aboard task get 12
aboard history 12
aboard events --task 12
```

CLI 写操作需要由写入方提供 actor 记录标签；该标签用于追溯操作来源，不代表身份认证：

```bash
export AGENT_BOARD_ACTOR=human
# 或在单条命令上使用 actor flag（按该命令 help 显示的形式）
```

创建一个未入队 Task：

```bash
aboard task create \
  -title "Fix stacked coupon calculation" \
  -description "..." \
  -acceptance "..."
```

Task 是否进入 READY，由 Human 或其明确委托者决定。

## Web

```bash
aboard web
```

Web Board 在 loopback 地址上选择可用端口。它适合 Human 浏览、编辑、入队、调整 READY 顺序、查看 Facts 与历史记录。

## MCP

MCP 不是使用前提。需要时可生成对应 Harness 的 stdio 配置：

```bash
aboard mcp config claude-code
aboard mcp config codex
aboard mcp config opencode
```

## 执行方式

同一个 Harness 也能同时承担管理端和执行端，但 Worker 与 Independent Verifier 应使用彼此独立的会话/实例。

有 Paseo / Orca 等 Orchestrator 时，可以让它持续读取 READY，安排 Worker 和新的 Independent Verifier。Agent Board 只记录 Task、状态和事实，不负责决定模型、Harness 或调度策略。
