# 使用 Agent 安装 Agent Board

将 README 中的提示交给 Codex、Claude Code 或 OpenCode。Agent 应依据当前系统和实际可用条件选择安装路径；不要把无法验证的步骤算作成功。

## 可复制的安装指令

```text
请按照 https://github.com/boboty/agent-board/blob/main/docs/install-with-agent.md 为当前环境安装并验证 Agent Board。

1. 先确认当前操作系统和架构、当前会话使用的 Harness，以及我指定的目标 Git 仓库。Harness 以当前会话/应用为准；只确认当前环境，不扫描其他 Harness 或无关仓库。目标仓库不明确时，先问我路径，不要猜测。
2. 检查 `aboard` 是否已安装且能运行。若没有，依据系统和架构选择 Agent Board Releases 中可用的预编译版本；如果本机已有 Go 1.25 或更新版本，也可使用 `go install github.com/boboty/agent-board/cmd/aboard@latest`。将 binary 放入已有 PATH 目录；如需调整 PATH，只改当前用户所用的必要配置，并报告所改路径。若两种路径都不可用，停止并说明缺少什么以及下一步。
3. 运行 `aboard version`，确认实际执行的 binary 路径和版本（可用 `command -v aboard`；Windows 使用当前 Shell 的等效命令）。如果新开的 Shell 才能识别 PATH，说明需要重新打开 Shell；未验证前不要报告 CLI 安装完成。
4. 运行 `aboard skill install` 和 `aboard skill check`。安装会把 `agent-board-management` 与 `agent-board-workflow` 放到当前支持的用户级目录：Codex 使用 `~/.agents/skills/`；Claude Code 与 OpenCode 共用 `~/.claude/skills/`。默认安装会补缺失项并保留内容不同的文件。对不同内容先报告其路径和 `different` 状态，不要加 `--force`；只有我明确授权覆盖对应 Skill 后，才运行 `aboard skill install --force management` 或 `aboard skill install --force workflow`，然后再次检查。
5. 在我指定的 Git 仓库目录运行 `aboard init`，再运行 `aboard doctor`。`init` 会在该仓库的 Git common directory 保存本机项目身份，并在用户数据目录创建 Board 数据；这些不是仓库工作树中的文件。若目标不是 Git 仓库、无法写入，或 doctor 失败，停止并报告精确原因和修复所需的下一步；不要清理或覆盖已有项目数据。
6. 最后逐项报告：已确认的系统/架构和当前 Harness；aboard 版本与 binary 路径；两个 Skills 在各支持目录的 `current`、`missing` 或 `different` 状态；目标仓库路径及 `init` 结果；`doctor` 的完整结果；未解决问题及下一步。只有上述必要检查都成功且目标项目已初始化，才称“安装并就绪”。安装不代表已定义/入队/执行 Task，也不授权 commit、push、merge 或 release。

安装完成后，告诉我如何开始定义第一个 Task，并指向 https://github.com/boboty/agent-board/blob/main/docs/first-task.md 。MCP 是可选入口；这条路径使用 CLI，不需要 MCP、Paseo、Orca 或其他调度产品。
```

## Agent 应如何判断安装结果

`aboard doctor` 会检查 binary、Skills 和当前项目配置。它以退出码 `0` 表示没有发现问题，退出码 `1` 表示有组件需要处理，退出码 `2` 表示命令用法错误。doctor 的 Skills 状态同时列出 Claude Code、Codex 和 OpenCode 的安装位置；当前实现中 Claude Code 与 OpenCode 指向同一份 `~/.claude/skills` 文件。doctor 是本地诊断，不会安装缺少的组件。

当前 Release 说明的平台为 macOS（Apple Silicon、Intel）、Linux（amd64、arm64）和 Windows（amd64）。若 Release 没有当前系统/架构的 binary，可在 Go 1.25+ 环境选择 Go 安装；否则报告未支持的组合及下一步，不要声称已安装。Go 安装后仍须实际确认 `aboard` 在当前 Shell 的 PATH 中。

Harness 路径依据当前 CLI 的 Skills 目标：Codex 使用 `~/.agents/skills/agent-board-<name>/SKILL.md`；Claude Code 与 OpenCode 共用 `~/.claude/skills/agent-board-<name>/SKILL.md`。`aboard skill check` 和 `doctor` 只检查这些文件的内容，不会启动 Harness 来确认它已加载 Skill。本指南没有实测三个 Harness 的实际发现/加载行为；若当前会话看不到 Skill，应报告“文件已安装、Harness 加载未验证”，并提示用户重新打开 Harness 或检查其 Skill 发现说明。

遇到旧版或不同来源的 Skill 时，`aboard skill check` 会显示状态和路径。普通 `skill install` 保留不同内容；`--force` 是覆盖授权，不是普通修复选项。参见[运维说明](operations.md)。
## 安装完成后

1. 在目标仓库运行 `aboard board` 或 `aboard web`，确认 Board 可读。
2. 让当前 Agent 使用 `agent-board-management` Skill 和你一起起草第一个 Task；确认完整定义后，由你明确授权入队为 `READY`。
3. 按[首次跑通一个 Task](first-task.md)走完一次 Worker → Independent Verifier → DONE 闭环。

日常命令见[日常使用](usage.md)。完整 workflow 规则以 [Workflow Skill](../workflow/SKILL.md) 和 [Management Skill](../management/SKILL.md) 为准。
