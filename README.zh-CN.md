# Agent Board · AI 研发工单制

[English](README.md) | [简体中文](README.zh-CN.md)

**任务有明确边界，交付有可核实的证据，验收保持独立，过程可追溯。**

Agent Board 是一套面向 AI 辅助软件研发的任务协作与交付验收机制。它帮助不同 Agent 围绕清晰的工单协作，留下可以在会话结束后继续使用的任务事实。

`aboard` 是它的本地参考实现：通过共享任务账本支持 Codex、Claude Code、OpenCode 等不同 Harness。它**不是**编程 Agent，也不负责调度 Agent。

## 快速开始

### 1. 让你的 Agent 安装（推荐）

在 Codex、Claude Code、OpenCode 等工具中复制：

> 请按照 [Agent Board 安装说明](docs/install-with-agent.md)，为我指定的 Git 仓库安装并验证 Agent Board。先确认操作系统、当前 Harness 和目标仓库；安装 `management`、`workflow` 两个 Skills，已有内容不同不得擅自覆盖；只在目标仓库执行 `aboard init` 和 `aboard doctor`。逐项报告成功、失败及阻塞情况。在我确认前不要创建或入队 Task，也不要执行、提交或推送任务。

完整步骤见[让 Agent 安装](docs/install-with-agent.md)。

**手工安装：** 从 [Releases](https://github.com/boboty/agent-board/releases) 下载 macOS、Linux 或 Windows 对应的 `aboard` 可执行文件并加入 `PATH`，然后运行：

```sh
aboard version
aboard skill install
cd /path/to/your/git-repository
aboard init
aboard doctor
aboard web
```

也可使用 Go 1.25+ 安装：

```sh
go install github.com/boboty/agent-board/cmd/aboard@latest
```

使用 Go 安装后，仍需执行 `aboard skill install` 和初始化命令。升级、故障处理见[运维说明](docs/operations.md)。

### 2. 跑通第一个 Task

告诉 Agent：

> 请把这个缺陷或功能建成一张尚未入队的 Board Task，写清范围和能够独立检查的验收标准。先不要开发。

确认工单定义后，再将其加入 **READY** 队列。然后告诉 Agent：

> 按照 `agent-board-workflow` 执行 READY Task。由 Worker 负责开发，使用全新的独立 Verifier 会话进行验收。遇到需要我决定的事项先暂停并报告。

执行 `aboard web`，即可查看 Task、交付记录、验收结论及历史事实。

[完整的第一个 Task 教程 →](docs/first-task.md)

![Agent Board 任务看板](docs/images/board.webp)

## 一张 Task 如何完成

1. **定义与授权。** 先明确目标、范围、约束和验收标准；创建 Task 不代表开工，进入 `READY` 才代表执行授权。
2. **执行与交付。** Worker 留下稳定的工作区，通过 `delivery` fact 记录基线、指纹、证据和局限。
3. **独立验收。** 全新、只读的 Independent Verifier 检查交付，记录 `PASS`、`RC`（退回修正）或 `BLOCKED`。Worker 自检不能替代独立验收。
4. **接受与收口。** 经验证的内容形成 accepted commit 后由 Orchestrator 收口；或由 Human 通过独立的 `decision` fact 明确接受。验收结论不会自动改变 Task 状态。

`RC` 后应生成新的交付并重新独立判断全部验收标准。只有核实对当前交付仍有效的历史证据才能复用；受影响或存在不确定性的检查必须重做。具体规则见[工作流规范](workflow/SKILL.md)和[RC 差异复验示例](docs/rc-evidence-examples.md)。

![工单交付、独立验收与事实记录](docs/images/task-detail.webp)

## 为什么不用 Agent 内部 TODO？

Agent 可以自己管理执行步骤，但跨 Agent、跨会话、跨 Harness 或跨 worktree 协作，还需要共享且可恢复的任务事实：

- **先有契约：** 开工前明确什么结果才能接受。
- **交付可识别：** 使用 baseline 和 fingerprint 关联被验收的具体内容。
- **职责相互独立：** Worker 不宣布 `PASS`，Human 的接受也不伪装成验收。
- **过程能追溯：** 执行、交付、验收、决策和交接不会随着会话结束丢失。

只有值得独立调度、验收或交接的工作才成为 Board Task；Worker 的内部计划和子步骤留在执行工具中。

## `aboard` 能做什么、不能做什么

| 当前提供 | 刻意不承担 |
| --- | --- |
| 本地 Task、`READY / IN_PROGRESS / DONE / BLOCKED` 和 READY 排序 | 启动 Agent、调度 Worker、选择模型 |
| 结构化交付和验收 Fact、审计历史 | 不经核实就判断 Agent 声称的结果正确 |
| 同一账本的 CLI、MCP 和 Web 入口 | 会话、执行计划与通用项目记忆管理 |
| 同一 Git clone 的多个 worktree 共享 Board | 跨机器共享账本或分布式节点网络 |

**Workflow Skill 定义协作规则，Board 记录任务事实。** Board 不认证 Agent 身份，也不在代码层强制执行每一条流程规范。通过 `aboard` 写入的 Fact 采用只追加语义，但本地 SQLite **不是防篡改存储**。

项目身份属于本机 Git clone，同一 clone 的 worktree 共享 Board；独立 clone 各自初始化。工程质量标准由独立的工程规范承担，不混进任务账本。

## 长期方向

当前产品首先服务于 AI 辅助软件研发。更长期的方向，是探索一种不绑定模型、Harness 或单一中心调度者的协作协议：**Goal 拥有账本，Task 承载契约，Delivery 携带证据，Governance 决定是否接受。** 这是设计方向，**不代表 v0.2.3 已经实现 Goal 对象或跨节点协作。**

[阅读项目白皮书 →](docs/WHITEPAPER.zh-CN.md)

## 更多文档

- [让 Agent 安装](docs/install-with-agent.md) · [第一个 Task](docs/first-task.md) · [日常使用](docs/usage.md)
- [工作流规范](workflow/SKILL.md) · [最佳实践](docs/best-practices.md) · [升级与运维](docs/operations.md)
- [产品定义](PRODUCT.md) · [技术架构](ARCHITECTURE.md) · [从源码开发](docs/development.md)
- [白皮书](docs/WHITEPAPER.zh-CN.md) · [Whitepaper (English)](docs/WHITEPAPER.md)

**许可证：** Apache-2.0。第三方代码归属详见 [LICENSE](LICENSE) 与 [NOTICE](NOTICE)。
