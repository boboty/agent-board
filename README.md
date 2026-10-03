# Agent Board · AI 研发工单制

[中文](README.md) | [English](README_EN.md)

**让交给 AI 的每一项工作，都有明确责任、有证据交付、有独立验收、有记录可追溯。**

Agent Board 首先是一套 **AI 研发工作制度**：规定什么工作值得成为工单、谁承担执行、什么算交付、谁可以独立验收，以及中断、返工和人的裁决如何留下可恢复、可查询的事实。

`aboard` 是这套制度的本地参考实现。它不管理智能体内部怎么思考，而保存那些必须跨智能体、跨会话、跨工作树留下来的任务事实。

## 为什么需要一套“制度”

模型越来越擅长规划、拆解、自检和管理自己的上下文。这些内部步骤可以继续留在 Codex、Claude Code、OpenCode 或其他执行工具里。

但一旦多个 AI 智能体跨会话、跨工作树（worktree）、跨工具协作，真正麻烦的问题变成了：

- 这项工作到底是什么，什么才算完成？
- 现在谁在做？中断以后谁可以接手？
- AI 说“做完了”，凭什么接受？
- 验收者检查的是不是执行者最后交付的那一份结果？
- 做的人能不能自己宣布验收通过？
- 退回修改以后改了什么，谁重新验证？
- 两周后回头看，当时交付了什么、谁验的、为什么放行？

这些问题不会随着模型变强而消失。它们不是智能问题，而是**分工、交接、验收和责任边界**的问题。

核心规则本身并不依赖软件开发，但研发是当前第一个、也是已经过实际项目验证的应用领域。其他领域是否采用同样的证据和验收细则，需要分别验证，而不是预设。

## 一张工单怎么走完

假设你在开发 `shop-api`，发现一个问题：满减券和折扣券叠加时多扣钱。

### ① 先定义工单，不开工

> 把这个问题建成工单，验收标准写清楚，先别开工。

工单创建后先由人确认。只有进入 `READY`，才代表获得执行授权。

### ② 执行者执行并交付

> 执行 READY 队列，遵循 `agent-board-workflow`。遇到需要我决定的事就停下报告。

执行者（Worker）自己管理内部计划和子步骤。完成后留下稳定的工作区、交付记录和验证证据，并明确这一轮交付对应哪一份结果。

### ③ 独立验收者验证

新的独立验收者（Independent Verifier）按照工单验收标准、完整差异和自己执行的检查重新验证。

执行者的交付说明只是**待核实的声明**，不能替代验收者自己的检查。

- `PASS`：这份交付通过独立验证
- `RC`：退回原工单修正，形成新的交付，再由新的独立验收者复验
- `BLOCKED`：当前无法完成验证，记录原因并停止

`PASS / RC / BLOCKED` 是 verification verdict，不是额外的工单状态，也不会自动改变工单状态。

人（Human）也可以明确接受一份没有独立验收者 `PASS` 的交付，但这会记录为 `decision`，而不是伪装成 `PASS`。

![工单详情：交付、独立验收、事实历史](docs/images/task-detail.webp)

你回来看到的不是“AI 说自己做完了”，而是：

**做了什么 → 交付的是哪一份结果 → 谁独立验过 → 为什么可以接受。**

## 核心规则

1. **可独立委托的工作，一事一单。** 只有能被独立调度、独立验收、必要时独立交接的工作才进入 Board。执行者自己的待办、计划、子步骤和子智能体拆分留在执行工具内部。
2. **先明确边界，再授权执行。** 工单写清目标、范围和可检查的验收标准；创建工单不等于开工，进入 `READY` 才代表获得执行授权。同一工单的工作区，同一时刻最多只有一个当前执行者。
3. **交付必须有证据，也必须能被识别。** “做完了”不是结论。执行者要记录改了什么、基于什么基线、做了哪些检查、哪些内容没有验证，以及这一轮交付对应哪一份结果。
4. **做的人不验收。** 执行者可以自检，但不能自己宣布 `PASS`。正式验收由新的独立会话或实例完成；验收以验收标准、完整差异和验收者自己的检查为准。
5. **验证结论和人的裁决分开。** `PASS` 只属于独立验收者。人可以接受一份未通过独立验收的交付，但只能记录为 `decision`。
6. **返工不另起炉灶。** `RC` 后继续在原工单和原交付边界上修正；修正后形成新的交付，再由新的独立验收者完整复验。
7. **状态不靠任何一个智能体的记忆。** 中断或接管时，从工单与 Board facts、Workspace/Git 和必要的 Agent activity 中恢复事实；handoff 只补充缺失上下文，不另建第二套进度事实源。

调度者（Orchestrator）负责安排工作和恢复流程，不承担工单执行，也不做独立验收；执行者负责执行和交付；独立验收者不修改交付内容，只负责验证并记录结论。

人始终拥有产品意图、READY 优先级，以及 merge、push、release 等不可逆动作的最终授权权。

## 参考实现：`aboard`

Agent Board 把这些规则落到一份外部、持久、可追溯的本地任务账本里。

**代码提供能力，Skill 定义规则，Board 保存事实。**

![Agent Board](docs/images/board.webp)

- **外部事实源**：任务状态不寄托在某个智能体的上下文、TODO 或进度文件里
- **明确的交付与验收身份**：delivery 和 verification 使用结构化核心字段，可以机械判断验收对应哪一份交付
- **独立验收事实**：执行者交付，新的独立验收者验证；独立验收者 `PASS` 与人的 `decision` 分开记录
- **可追溯来源**：事实可以记录 role、session、harness、model；`actor` 表示实际写入者，不是授权者，也不代表经过身份认证
- **跨工具协作**：CLI、MCP 和 Web Board 操作同一份事实，不绑定 Claude Code、Codex、OpenCode 或特定模型
- **本地优先**：一个二进制程序 + SQLite，无账号、无托管服务、无数据库服务器

通过 `aboard` 写入的 facts 采用只追加、不覆盖的语义。Agent Board 不提供防篡改存储；拥有本机文件权限的进程仍然可以直接修改 SQLite 数据库。

工单生命周期刻意保持简单：

- `READY`：待执行
- `IN_PROGRESS`：执行中
- `DONE`：已完成
- `BLOCKED`：阻塞

执行者、验收者、RC、handoff、session 都不是额外的顶层状态。

## 它不是什么

Agent Board 不是 IDE，不是 AI 智能体，不是 Agent Harness，也不是调度器。

它不接管执行者的内部计划，不决定模型和 Harness，不自动拉起 Agent，也不会因为 `PASS` 或 `RC` 自动改变工单状态。

你可以继续在原来的工具里讨论和开发，也可以结合 Paseo、Orca 或其他调度工具做并发执行。具体 Harness 运行方式见 [最佳实践](docs/best-practices.md)。

## 它和 GitHub Issues / PR 是什么关系

Agent Board 不试图替代 GitHub。

GitHub Issues、PR、CI 和 Code Review 非常适合代码托管平台内的协作和最终收口；Agent Board 关注的是更靠近执行现场的一层：**本地、多工作树、多执行工具、多智能体之间的执行、交接和独立验收事实。**

如果团队能够直接用 GitHub 或其他平台完整承载同样的规则，也没有问题。规则不应该依赖 Agent Board 才成立。

`aboard` 的价值，是提供一个轻量、本地、可直接被 AI 操作的参考实现。

## 和常见做法相比

| | 文件 / Agent 内部 TODO | Issues / PR | **Agent Board** |
|---|---|---|---|
| 本地多 Agent 共享任务事实 | 需要约定，容易分叉 | 以远端协作为主 | 原生 |
| 跨 Harness 执行与交接 | 通常局限于当前工具 | 需要额外集成 | 原生记录 |
| 独立验收作为本地执行事实 | 需自行约定 | 可通过 PR / Review 实现 | 原生记录 |
| AI 直接读写 | 文件或工具内部能力 | API + 鉴权 | CLI / MCP |
| 运行依赖 | 无 | 远端服务 | 本地 binary + SQLite |

## 5 分钟开始

### 下载预编译版本

从 [GitHub Releases](https://github.com/boboty/agent-board/releases) 下载对应平台的 `aboard`，放入 `PATH`：

- macOS — Apple Silicon / Intel
- Linux — amd64 / arm64
- Windows — amd64

Release 同时提供 `SHA256SUMS`。

然后：

```bash
aboard version
aboard skill install

cd your-project
aboard init
aboard doctor
aboard web
```

### 或使用 Go 安装

本机已有 **Go 1.25+** 时：

```bash
go install github.com/boboty/agent-board/cmd/aboard@latest
aboard skill install

cd your-project
aboard init
aboard doctor
aboard web
```

`aboard init` 必须在 Git 仓库内运行。项目身份保存在本机 Git common dir（普通 clone 中为 `.git/agent-board.json`），不进入工作树、不提交；Board 数据保存在 `~/.agent-board/<project_id>/board.db`。

同一本地仓库的所有 worktree 自动共享同一个 Board；其他机器上的独立 clone 需要各自运行一次 `aboard init`。

从 v0.1.x 升级时，项目身份位置已经调整，请先阅读 [升级与运维说明](docs/operations.md)。

之后回到你的 AI 工具，用自然语言说“把 XX 建成工单”即可。

### 执行端怎么选

- **单一 Claude Code / Codex / OpenCode**：也能使用；执行者和独立验收者使用彼此独立的会话或实例
- **并行或后台执行**：可以结合 Paseo、Orca 或其他调度者读取 READY，并安排执行者和新的独立验收者

是否自动 merge、push 或 release，由人在每次运行中明确授权。

## 你始终保留控制权

- 创建工单不等于开工：只有进入 `READY` 才获得执行授权
- 执行者无权用“自检通过”替代独立验收
- 独立验收者不修改交付内容
- verification verdict 不自动改变工单状态
- 人的接受不冒充独立验收者 `PASS`
- 语义不清、冲突或异常时停止并记录，必要时进入 `BLOCKED`
- merge、push、release 等不可逆动作是否自动完成，由人在本次运行中明确授权

Agent Board 不是把人从研发里拿掉，而是把人的注意力从盯执行过程移回**做什么、先做什么，以及出现分歧时如何裁决**。

## 更多

- [工作流规则：完整规范](workflow/SKILL.md)
- [日常使用：CLI / MCP / Skills](docs/usage.md)
- [最佳实践：Harness 与调度运行方式](docs/best-practices.md)
- [升级、清理、卸载](docs/operations.md)
- [PRODUCT.md](PRODUCT.md) — 产品定义与边界
- [ARCHITECTURE.md](ARCHITECTURE.md) — 架构与持久化
- [从源码开发](docs/development.md)

Apache-2.0。部分底层基础设施代码源自 [rhizome-mcp](https://github.com/Odrin/rhizome-mcp)，依据 Apache-2.0 使用与修改，详见 [NOTICE](NOTICE)。
