# Agent Board

[中文](README.md) | [English](README_EN.md)

**AI 干活不能只留下结果，还必须留下“为什么这个结果可以被接受”的证据链。**

Agent Board 是一套 **AI 研发工单制** 的本地参考实现：用外部持久化工单记录任务、执行、交付证据、独立验收和人的裁决，让工作能够跨 Agent、跨会话、跨 Harness、跨 worktree 可靠交接。

**看板只是视图。真正的核心是：规则 + 工单账本 + 证据与裁决。**

![Agent Board](docs/images/board.webp)

- **外部事实源**：不把任务状态寄托在某个 Agent 的上下文、todo 或 `PROGRESS.md` 里
- **独立验收**：Developer 负责交付，另一个全新、独立的 Verifier 负责判断是否 PASS
- **证据可复查**：DONE 不只是一列状态；交付了什么、验了什么、为什么放行都有记录
- **跨 Harness**：Claude Code / Codex / OpenCode 等可以通过 CLI / MCP 操作同一份工单事实
- **本地优先**：一个二进制 + SQLite，无账号、无服务、无数据库服务器

## 为什么需要一份外部工单

Agent 越来越擅长自己规划、拆解、自检和管理内部上下文。这些事情可以留在 Harness 里。

但下面这些事实不能只存在于某个 Agent 的记忆里：

- 这项工作到底是什么，验收标准是什么
- 谁接过、谁执行过，中断后由谁接管
- Developer 实际交付了什么
- Verifier 独立检查了什么，依据是什么
- 为什么 PASS、为什么 RC、为什么 BLOCKED
- 哪一步需要 Human 授权或裁决

这些不是“模型够不够聪明”的问题，而是**交接、验收和责任边界**的问题。

> **Agent 管自己的内部步骤；Board 只记录必须跨执行者、跨会话保留下来的共享事实。**

## 从治理角度，只需要看四类事实

| | 回答的问题 |
|---|---|
| **工单 Task** | 要做什么？什么算完成？ |
| **执行 Execution** | 谁做过？在哪个 Harness / worktree 做？是否发生过接管？ |
| **证据 Evidence** | Developer 交付了什么？Verifier 实际验证了什么？ |
| **裁决 Verdict** | PASS、RC、BLOCKED，还是需要 Human 决定？ |

实现层面的任务状态仍然保持简单：`READY` / `IN_PROGRESS` / `DONE` / `BLOCKED`。

Developer、Verifier、RC、handoff、session 都不是额外的顶层状态；它们是围绕工单发生的执行和审计事实。

## 规则先于工具

Agent Board 把两件事刻意分开：

### Workflow Skill：定义规则

- Task 如何定义、何时进入 READY
- Orchestrator / Developer / Independent Verifier 各自负责什么
- delivery 后如何独立验收
- RC 如何回流、如何重新验证
- 中断、接管和 stop-for-decision 怎么处理
- 什么情况下才算真正完成

### Shared Task Board：记录事实

- Task 和显式任务状态
- READY 顺序
- Harness / model / worktree 等执行信息
- delivery evidence
- verification evidence
- blocked reason
- handoff / takeover
- audit history

**代码提供能力，Skill 定义规则。Board 是 ledger，不是 workflow engine。**

## 一张工单怎么走完

假设你在开发 `shop-api`，发现一个 bug：满减券和折扣券叠加时多扣钱。

**① 先定义工单，不开工**

> 把这个 bug 建成 Board Task，验收标准写清楚，先别开工。

Task 创建后先由 Human 看一眼。只有进入 READY，才代表它获得执行授权。

**② Developer 执行并交付证据**

> 执行 READY 队列，遵循 `agent-board-workflow`。遇到需要我决定的事就停下报告。

Developer 可以自己规划内部步骤，但最终必须留下可复查的交付事实和证据。

**③ Independent Verifier 独立验收**

由一个**全新、独立会话/实例**的 Verifier 按 Task 和验收标准复核，而不是让 Developer 自己宣布完成。

- PASS → 接受交付 → DONE
- RC → 回 Developer 修正 → 再由新的独立 Verifier 复验
- 需要 Human 决策 → BLOCKED，并记录原因

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

Agent Board 的作用，是提供一个轻量、本地、可直接被 Agent 操作的参考实现。

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

## 它刻意不做的事

不做模型路由，不做 Agent runtime，不调度 Agent，不接管 Agent 的内部 todo，不要求你换 IDE 或 Harness。

它只负责一件事：

> **让 AI 工作在不同执行者之间交接时，任务、证据、验收和裁决不会随着某个会话一起消失。**

## 更多

- [日常使用：CLI / MCP / 两个 Skill](docs/usage.md)
- [升级、迁移、清理、卸载](docs/operations.md)
- [PRODUCT.md](PRODUCT.md) — 产品定义与边界
- [ARCHITECTURE.md](ARCHITECTURE.md) — 架构与持久化
- [从源码开发](docs/development.md)

Apache-2.0
