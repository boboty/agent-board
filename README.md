# Agent Board · AI 研发工单制

[中文](README.md) | [English](README_EN.md)

**让交给 AI 的每一项工作，都有明确责任、有证据交付、有独立验收、有记录可追溯。**

Agent Board 首先是一套 **AI 研发工作制度**：规定什么工作值得成为工单、谁负责执行、什么算交付、谁有资格验收，以及中断、返工和人的裁决如何留下记录。

这套制度被写成 harness-independent 的 Workflow Skill；`aboard` 是它的本地参考实现，让不同 Agent、不同会话、不同 Harness 共享同一份任务事实。

## 为什么需要一套“制度”

模型越来越擅长规划、拆解、自检和管理自己的上下文。这些内部步骤可以继续留在 Codex、Claude Code、OpenCode 或其他 Harness 里。

但一旦多个 Agent 跨会话、跨 worktree、跨工具协作，真正麻烦的问题变成了：

- 这项工作到底是什么，什么才算完成？
- 现在谁在做？中断以后谁可以接手？
- Agent 说“做完了”，凭什么接受？
- 做的人能不能自己宣布验收通过？
- RC 以后改了什么，谁重新验证？
- 两周后回头看，当时交付了什么、谁验的、为什么放行？

这些问题不会随着模型变强而消失。它们不是智能问题，而是**分工、交接、验收和责任边界**的问题。

人类研发团队靠工单、Review、验收和审计解决这类问题。AI 研发也需要一套明确、能被 Agent 自己遵循的规则。

## 核心规则

1. **可独立委托的工作，一事一单。** 只有能被独立调度、独立验收、必要时独立交接的工作才进入 Board。Developer 自己的 todo、plan、sub-step 留在 Harness 内部。
2. **先明确边界，再授权执行。** Task 要写清目标、范围和可检查的验收标准；创建 Task 不等于开工，进入 `READY` 才代表它可以执行。同一 Task workspace 同一时刻最多只有一个当前 Developer。
3. **交付必须附证据。** “做完了”不是结论。Developer 要记录改了什么、基于什么 baseline、做了哪些检查、结果是什么、哪些内容没有验证以及原因。
4. **做的人不验收。** Developer 可以自检，但不能宣布 PASS。每一轮正式验收都由一个新的、独立会话/实例的 Verifier 完成，结论是 `PASS`、`RC` 或 `BLOCKED`。
5. **返工不另起炉灶。** RC 回到原 Task 和原交付边界继续修正；修正后产生新的 delivery，再由新的 Independent Verifier 完整复验。交付、验收和 handoff 事实只追加、不覆盖。
6. **状态不靠任何一个 Agent 的记忆。** 中断或接管时，从 Task / Board facts、Workspace / Git 和 Agent activity 恢复事实；必要的 handoff 只补充缺失上下文，不另建一份 `PROGRESS.md`。

Human 始终拥有产品意图、READY 优先级以及 push / merge / release 等不可逆动作的最终授权权。Orchestrator 负责调度和记录状态，不写实现、不做验收；Developer 负责实现；Independent Verifier 只读验收。

> **Agent 管自己的内部步骤；制度只约束必须跨执行者保留下来的责任、证据和裁决。**

## 参考实现：`aboard`

Agent Board 把上面的规则落到一份外部、持久、可审计的任务账本里。

**看板只是视图。真正的核心是：规则 + 工单账本 + 证据与裁决。**

![Agent Board](docs/images/board.webp)

- **外部事实源**：任务状态不寄托在某个 Agent 的 context、todo 或进度文件里
- **独立验收**：Developer 交付，新的 Independent Verifier 验证
- **证据可复查**：delivery / verification / handoff / decision 都有历史记录
- **跨 Harness**：CLI / MCP / Web 操作同一份事实，不绑定 Claude Code、Codex、OpenCode 或特定模型
- **本地优先**：一个二进制 + SQLite，无账号、无托管服务、无数据库服务器

实现层面的 Task 状态刻意保持简单：`READY` / `IN_PROGRESS` / `DONE` / `BLOCKED`。

Developer、Verifier、RC、handoff、session 都不是额外的顶层状态；它们是围绕 Task 发生的执行和审计事实。

## 它不是什么

不是 IDE，不是 Agent，不是 Harness，也不是调度器。

你继续在原来的工具里讨论和开发，也可以继续用 Paseo 或其他 Orchestrator 做并发调度。Agent Board 不接管 Agent 的内部计划，不做模型路由，也不试图把所有流程塞进 Board。

**代码提供能力，Skill 定义规则。Board 是 ledger，不是 workflow engine。**

## 一张工单怎么走完

假设你在开发 `shop-api`，发现一个 bug：满减券和折扣券叠加时多扣钱。

**① 先定义工单，不开工**

> 把这个 bug 建成 Board Task，验收标准写清楚，先别开工。

Task 创建后先由 Human 看一眼。只有进入 READY，才代表它获得执行授权。

**② Developer 执行并交付证据**

> 执行 READY 队列，遵循 `agent-board-workflow`。遇到需要我决定的事就停下报告。

Developer 自己管理内部计划，完成后留下稳定 workspace、delivery facts 和足够的验证证据。

**③ Independent Verifier 独立验收**

由一个**全新、独立会话/实例**的 Verifier 按 Task、完整 diff 和验收标准复核，而不是让 Developer 自己宣布完成。

- PASS → 接受已验证的交付 → 封装 accepted commit → DONE
- RC → 回 Developer 修正 → 形成新 delivery → 由新的 Independent Verifier 复验
- BLOCKED / 需要 Human 决策 → 停下并记录原因

![Task 详情：交付、独立验证、审计历史](docs/images/task-detail.webp)

你回来看到的不是“Agent 说自己做完了”，而是：

**做了什么 → 留了什么证据 → 谁独立验过 → 为什么可以接受。**

人的注意力只留给三件事：**做什么、先做什么、分歧怎么裁决。**

## 它和 GitHub Issues / PR 是什么关系

Agent Board 不试图替代 GitHub。

GitHub Issues / PR / CI / Review 非常适合代码托管平台内的协作和收口；Agent Board 关注的是更靠近执行现场的那一层：**本地、多 worktree、多 Harness、多 Agent 之间的执行、交接和独立验收事实。**

语义上可以自然映射：

- Board Task ↔ Issue / work item
- Developer delivery ↔ implementation / PR candidate
- Verification evidence ↔ CI / review evidence
- PASS / Closure ↔ accepted delivery / merge boundary

如果你的团队最终能直接用 GitHub 或其他平台完整承载同样的规则，也没有问题。**规则不应该依赖 Agent Board 才成立。**

`aboard` 的价值，是提供一个轻量、本地、可直接被 Agent 操作的参考实现。

## 和常见做法相比

| | TODO.md / PROGRESS.md | Agent 自带 todo | Issues / Linear | **Agent Board** |
|---|---|---|---|---|
| 跨会话、跨 worktree 共享 | 需要约定，容易分叉 | 通常局限于单次会话或 Harness | ✓ | ✓ |
| 状态依据 | 文件内容 | Harness / 会话内部状态 | 显式字段或自动化 | 显式记录，带操作者和版本号 |
| “做完了”有没有证据 | 需自行约定 | 通常没有独立验收证据 | 依赖评论 / CI / Review | 交付 + 独立验证事实 |
| Agent 能直接读写 | 能，但并发时容易冲突 | 通常只服务当前 Harness | 需要 API / 鉴权 | CLI / MCP，带乐观锁 |
| 本地执行阶段的交接 | 需人工约定 | 通常不跨 Harness | 通常围绕远端 Issue / PR | 原生记录 handoff / audit facts |
| 部署成本 | 无 | 无 | 账号 / 网络 / 服务 | 本地二进制 + SQLite |

## 5 分钟开始

要求：**Go 1.25+**。

```bash
go install github.com/boboty/agent-board/cmd/aboard@latest
aboard skill install

cd your-project
aboard init
aboard doctor
aboard web
```

`aboard init` 会生成 `.agent-board.json`，建议提交进仓库。然后回到你的 AI 工具里，用自然语言说“把 XX 建成 Board Task”即可。

**执行端怎么选**

- **只有一个 Claude Code / Codex**：也能用；Developer 和 Verifier 使用彼此独立的会话/实例
- **想放后台、多任务并行**：可以用 [Paseo](https://github.com/getpaseo/paseo) 这类 Orchestrator 读取 READY 队列，安排 Developer 和 Independent Verifier

想让执行端连 merge / push 也做掉，在本次运行里明确授权即可；不授权就停在已验收的交付边界。

## 你始终保留控制权

- Task 创建不等于开工：只有进入 READY，才表示它可以执行
- 语义不清、冲突、异常 → 停下并记录，必要时进入 BLOCKED
- Developer 无权用“我自检通过”替代独立验收
- merge / push 是否自动完成，由 Human 在本次运行里授权

Agent Board 不是把 Human 从研发里拿掉，而是把 Human 从**盯过程**里拿掉，把注意力留给授权和裁决。

## 更多

- [Workflow Skill：完整规则](workflow/SKILL.md)
- [日常使用：CLI / MCP / 两个 Skill](docs/usage.md)
- [升级、迁移、清理、卸载](docs/operations.md)
- [PRODUCT.md](PRODUCT.md) — 产品定义与边界
- [ARCHITECTURE.md](ARCHITECTURE.md) — 架构与持久化
- [从源码开发](docs/development.md)

Apache-2.0
