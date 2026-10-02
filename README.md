# Agent Board

[中文](README.md) | [English](README_EN.md)

**Agent 说“做完了”不算数，独立验收说 PASS 才算。**

一块给 Claude Code / Codex / OpenCode 用的本地任务看板：你排好队列，Agent 领任务、实现，再由另一个独立 Agent 验收，全过程留痕。你只在需要拍板时回来。

![Agent Board](docs/images/board.webp)

- **不用再盯进度**：任务状态、交付、验收都在 Board 上，换会话、换 worktree 不用重讲“做到哪了”
- **验收有证据**：DONE 背后是谁交付、谁独立验证、验了什么，一条条可追溯
- **不绑工具**：CLI / MCP / Web 三个入口操作同一块 Board；管理和执行可以来自不同 Harness
- **本地优先**：一个二进制 + SQLite，无账号、无服务、无数据库要装

## 一次完整流程长什么样

假设你在开发 `shop-api`，发现一个 bug：满减券和折扣券叠加时多扣钱。

**① 在 Claude Code / Codex 里说一句**

> 把这个 bug 建成 Board Task，验收标准写清楚，先别开工。

Task 出现在 Board 的“未入队”区。你看一眼，点“入队”，它进入 READY。

**② 让执行端开工**

> 执行 READY 队列，遵循 `agent-board-workflow`。遇到需要我决定的事就停下报告。

Developer 实现 → 一个**全新、独立会话/实例**的 Verifier 按验收标准复核 → PASS 后封装成 commit → DONE。

**③ 你回来看到的是结果，不是过程**

![Task 详情：交付、独立验证、审计历史](docs/images/task-detail.webp)

- 优惠券那张卡：Developer 交付，Independent Verifier 验证 PASS，验证内容和证据都留在 Task 里
- Redis 迁移那张卡停在 BLOCKED：*需要 Human 确认灰度窗口*——这才是你需要花时间的地方

你只负责三件事：**做什么、先做什么、分歧怎么裁决。**

## 和你现在用的东西比

| | TODO.md / PROGRESS.md | Agent 自带 todo | Issues / Linear | **Agent Board** |
|---|---|---|---|---|
| 跨会话、跨 worktree 共享 | 需要约定，容易分叉 | 通常局限于单次会话或 Harness | ✓ | ✓ |
| 状态依据 | 文件内容 | Harness / 会话内部状态 | 显式字段或自动化 | 显式记录，带操作者和版本号 |
| “做完了”有没有证据 | 需自行约定 | 通常没有独立验收证据 | 依赖评论或自动化 | 交付 + 独立验证事实 |
| Agent 能直接读写 | 能，但并发时容易冲突 | 通常只服务当前 Harness | 需要 API / 鉴权 | CLI / MCP，带乐观锁 |
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

- **只有一个 Claude Code / Codex**：也能用 Agent Board；Developer 和 Verifier 应使用彼此独立的会话/实例
- **想放后台、多任务并行**：可以用 [Paseo](https://github.com/getpaseo/paseo) 这类 Orchestrator 读取 READY 队列，安排 Developer 和 Independent Verifier

想让执行端连 merge / push 也做掉，在本次运行里明确授权即可；不授权就停在已验收的交付边界。

## 你始终保留控制权

- Task 建了不等于开工：只有进入 READY，才表示它可以执行
- 语义不清、冲突、异常 → 执行端停下，写明原因，必要时进入 BLOCKED
- merge / push 是否自动完成，由你在本次运行里授权

Agent Board 不是把 Human 从研发里拿掉，而是把 Human 从**盯过程**里拿掉。

## 它刻意不做的事

不做模型路由，不做 Agent runtime，不调度 Agent，不要求你换 IDE 或 Harness。它只做一件事：

> **在任务管理和任务执行之间，提供一个稳定、共享、可审计的交接面。**

## 更多

- [日常使用：CLI / MCP / 两个 Skill](docs/usage.md)
- [升级、迁移、清理、卸载](docs/operations.md)
- [PRODUCT.md](PRODUCT.md) — 产品定义与边界
- [ARCHITECTURE.md](ARCHITECTURE.md) — 架构与持久化
- [从源码开发](docs/development.md)

Apache-2.0
