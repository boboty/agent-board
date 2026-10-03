# Agent Board 最佳实践

这里记录经过实际使用验证、但**不属于 Agent Board 工作流强制规则**的运行经验。

工作流规则回答“什么样的交付可以被接受”；本文件回答“在具体执行工具里，怎样运行得更稳、更省、更顺手”。不同 Harness 可以采用不同做法，只要不破坏 `agent-board-workflow` 的责任、交付与独立验收边界。

## 1. Paseo：事件驱动的 Orchestrator

当 Orchestrator 通过 Paseo 启动 Worker 或 Verifier 后，不需要持续驻留、轮询或周期性报告“仍在等待”。

推荐模式是：

```text
读取 Board
→ 做出一次调度决策
→ 记录必要事实
→ 启动 Worker / Verifier
→ 结束当前调度回合
→ 等待 Paseo completion / failure / permission 等事件
→ 恢复原 Orchestrator 会话
→ 读取最新事实并继续调度
```

可以概括为：

> **Delegate, persist, suspend. Resume only on events.**  
> **委托、落账、挂起；只在事件到来时恢复。**

这样做可以避免没有新信息时反复重新采样 Orchestrator，降低无意义的 Harness Tax。Worker 执行 5 分钟还是 50 分钟，都不应该因此产生额外的调度回合。

### 推荐启动模板

下面是一份 Paseo 下经过实际使用验证的启动模板。它只是运行建议，不是工作流协议的一部分：

```text
从当前项目 Agent Board 读取并执行 READY Tasks，遵循 agent-board-workflow。使用 Paseo 已配置的 Agent Profiles。Worker 使用 Developer；持续处理直到 READY 队列为空，或遇到需要 Human 裁决的事项时停止并报告。

如果任务完成并验收通过，自动合入 main、push origin/main，push 成功后清理对应的本地 worktree 和分支；遇到冲突或异常时停止并报告。

另外注意：本会话作为调度者，启动 Paseo Worker 或 Verifier 后，不要主动等待、轮询、查询其状态或周期性报告等待进度。保持 notify-on-finish，由当前调度回合立即结束。仅在 Paseo 的 completion、failure、permission 或其他事件重新激活本会话后继续调度。只有怀疑 Agent 已失联且没有正常事件时，才主动查询状态。
```

### 为什么这不写进 Workflow Skill

这段模板同时包含三类信息：

- **工作制度**：例如 Worker 与 Independent Verifier 的职责边界。这部分已经由 `agent-board-workflow` 定义。
- **Harness 绑定**：例如 Paseo 中 Worker 使用 Developer Profile、依靠 notify-on-finish 恢复调度。这是具体执行环境的行为。
- **本次运行授权**：例如是否允许自动 merge、push 和 cleanup。这应由 Human 在每次 run 启动时明确授权，而不应永久固化进工作流规则。

因此推荐保持：

```text
Workflow Skill   → 固化制度
Agent Board      → 固化事实
启动 Prompt       → 固化本次运行方式与 Human 授权
```

### 异常路径

事件驱动不等于永远禁止状态检查。

正常情况下不要 polling；只有出现具体迹象表明 Agent 可能失联，而且预期的 completion / failure / permission 事件没有到达时，Orchestrator 才主动查询 Agent 状态。

即：

```text
正常路径：event-driven
异常路径：主动检查
```

而不是：

```text
正常路径：polling
异常路径：more polling
```

### 不依赖 Orchestrator 常驻

Agent Board 的目标之一，就是让任务事实不依赖任何单一会话的记忆。

如果原 Orchestrator 会话能够 suspend/resume，优先恢复原会话；即使原会话丢失，也应能由新的 Orchestrator 通过 Board facts、Workspace/Git 和 Agent activity 恢复工作。

因此，Orchestrator 常驻是运行时便利，不应成为流程正确性的前提。

## 2. Self-hosting Board 的升级顺序

以下是 #27 self-hosting 实际验证得到的操作经验，属于非强制最佳实践。Agent Board 的候选版本尚未独立验收前，不要升级承载自身研发流程的真实 Board。数据库 migration 先在临时数据库或真实数据库副本验证；候选版本通过独立验收、accepted commit 已生成并完成合入或 push 后，再升级本机 binary、Skill、schema 和 Web。升级控制面前先备份；升级后检查 SQLite integrity、foreign keys 和数据一致性，并重启 Board 服务。

注意：当前 `aboard check` 打开数据库时会执行待办 migration，因此 schema 开发期间不要把它当作纯只读诊断命令。
