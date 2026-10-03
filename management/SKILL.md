---
name: agent-board-management
description: 管理 Agent Board 议程与 Task 定义。用户要求录入工作、完善 Task、接受入队为 READY 或调整 READY 优先级时使用。若用户要求实现、验证、交接或完成 Task，使用现有 Agent Board Workflow Skill。
---

# Agent Board 管理 Skill

本 Skill 指导如何维护 Board 议程和 Task 定义，可独立使用，不依赖 Agent Board Workflow Skill。Board 是记录 Task 的台账；它不会判断谁有权限，也不会替人判断定义是否就绪。

## 授权与职责边界

- Human 可以定义、接受、编辑 Task，将 Task 入队，并设置 READY 优先级。
- 代理只有在 Human 明确委托相应权限后，才能执行这些写操作。Human 授权来自当前请求或上下文，不通过 `actor` 表示。
- `actor` 是实际执行 Board mutation 的人或代理的自报自由文本标签，用于追溯执行者，不代表授权来源或身份认证。Human 亲自操作时可使用 `human/...`；受托代理执行 create/update/queue/reorder 等写操作时，必须使用自己的可追溯 actor，例如 `delegate/codex-<session>`，不得冒用 Human actor。
- 仅要求录入或入队时，不启动 Worker，不把 Task 设为 `IN_PROGRESS`，不记录 `execution` fact，不修改实现代码，也不提交、推送或合并。
- 定义尚未完整或未获授权 Human/受托代理接受时，保持为草稿。`create_task` 创建的 Task 未入队且没有生命周期状态；`queue_task` 会记录 `READY` 并将 Task 追加到 READY 顺序末尾。
- Task 状态仅有 `READY`、`IN_PROGRESS`、`DONE`、`BLOCKED`。本 Skill 只将 `READY` 用于已接受并入队的工作，不指导 Task 的执行。

## 定义和判断 Task 是否就绪

写入前先阅读相关 Board 事实并确认 Human 的意图。Task 只保留模型无法安全自行推断的目标、边界、关键决策、非目标和可验证验收标准；避免重复背景、重复约束、实现提示，以及把输入原样改写成长 Task。合格的 Task 定义包括：

- **title**：简短描述预期结果。
- **description**：说明目标、边界和关键决策，包含必要的非目标、约束或依赖。
- **acceptance criteria**：用于判断完成的具体、可检查条件，并明确证明正确性与安全性所需的最小充分证据。

Description 与 Acceptance Criteria 各司其职，不重复彼此内容。

就绪表示定义足以开始实现并独立验证，依赖已完成或明确说明为何可继续，并且没有未解决且会阻塞工作的产品决策。缺少影响就绪状态的决定时，向 Human 澄清；不要自行编造结论或擅自入队。若 Human 只要求起草，就创建或展示未入队的草稿。

遵循 Task 编写指导：验收标准应给出与预期改动半径和风险相称的验证边界，使用证明正确性与安全性所需的最小充分证据；不要要求重新证明不受影响的既有系统能力，不要把具体测试实施方案写成结果要求，也不要要求修改真实用户环境作为交付门槛。外部集成可用官方规则、隔离复现和必要的只读证据作为充分依据。

## 安全写入 Board

每次更新或入队写操作都使用刚读到的 Task `version` 作为 `expected_version`，并明确记录实际执行者的 actor。每次写入后重新读取 Task，确认字段和状态。

若写入结果未能观察到，只能使用相同请求和相同幂等键重试；同一幂等键不得用于不同请求。遇到版本冲突时，重新读取 Task 和近期 Board 事实，核对请求后再决定如何写入，禁止盲目重试。

重排前先读取完整 READY 队列及队列版本。提交期望顺序时必须包含当下所有 READY Task，且每项恰好一次；写入后重新读取队列确认。除非 Human 明确授权改变优先级，否则保留所有既有 READY Task 及其相对顺序。新增 Task 使用 `queue_task` 的末尾追加行为，不改变既有状态或顺序。

## CLI 路径（不需要 MCP）

从已初始化的项目目录运行命令。通用选项 `--dir`、`--actor`、`--key` 放在所作用的命令之后。`aboard operations` 可查看可用 operation 和输入 schema；以下示例使用当前 CLI 语法，由已获相应 Human 授权的代理执行，`delegate/codex-session` 应替换为代理自己的可追溯 actor。

创建草稿后读取并记录 Task 编号和版本：

```sh
aboard task create --title "文档导入限制" --description "单次文档导入上限为 10 MiB；本次不调整其他导入规则。" --acceptance "不超过 10 MiB 的文档通过大小检查；超过上限时拒绝导入并提示上限。" --actor delegate/codex-session --key ab-task-create-unique
aboard task get 12
```

读取当前版本后再编辑；编辑后重新读取确认：

```sh
aboard task update 12 --version 1 --description "单次文档导入上限为 10 MiB，适用于所有支持的文档格式；本次不调整其他导入规则。" --actor delegate/codex-session --key ab-task-edit-unique
aboard task get 12
```

获授权 Human 接受已就绪的定义时，先读取版本，再入队（该操作会追加到末尾），最后确认 Task 和队列：

```sh
aboard task get 12
aboard task queue 12 --version 2 --actor delegate/codex-session --key ab-task-queue-unique
aboard task get 12
aboard ready list
```

Human 授权调整优先级时，先读取完整队列；根据返回的 `version` 提交完整目标顺序，而不只是移动的 Task。例如当前顺序为 `12, 8, 4`、队列版本为 `7`：

```sh
aboard ready reorder --version 7 8 12 4 --actor delegate/codex-session --key ab-ready-order-unique
aboard ready list
```

将示例编号、版本、actor 和唯一幂等键替换为刚读取和当前请求对应的值。队列为空时无需重排。响应丢失时，只有使用相同参数和幂等键才可重试。

## 可选 MCP 路径

MCP 是可选的结构化 adapter；授权边界、Task 语义、版本检查、末尾追加和回读要求与 CLI 完全相同。可在已初始化的项目目录通过 stdio 启动 `aboard mcp`，或采用 `aboard mcp config generic` 输出的 command、args、env 启动。每次写调用都明确提供实际执行者的 actor（受托代理例如 `delegate/codex-<session>`）；仅当配置的默认 actor 正确标识当前执行者时才可保留。

调用 `create_task` 时提供 `title`、`description`、`acceptance_criteria`、`actor` 和唯一 `idempotency_key`；然后用返回的 Task 编号调用 `get_task`。编辑时调用 `update_task`，传入 `task`、刚读到的 `expected_version`、需要变更的字段、actor 和幂等键；之后再次调用 `get_task`。获授权接受草稿后，用 Task 编号、当前版本、actor 和幂等键调用 `queue_task`，再调用 `get_task` 和 `list_ready` 确认；queue 会把 Task 追加到末尾。

调整优先级时调用 `list_ready`，记下队列 `version`，再调用 `reorder_ready`。其 `expected_version` 必须使用该版本，`tasks` 必须完整列出当前所有 READY Task，且每项恰好一次，按期望顺序排列；同时提供 actor 和唯一幂等键。之后再次调用 `list_ready` 确认。幂等重试和冲突后的重新读取规则与 CLI 相同。这些操作只管理 Task 定义和 READY 顺序，不启动 Task 执行。
