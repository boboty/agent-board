# Agent Board 运维

## 升级

```bash
go install github.com/boboty/agent-board/cmd/aboard@latest
aboard version
aboard doctor
```

如果 `doctor` 报告已安装 Skill 与当前 binary 内嵌内容不同，可显式覆盖：

```bash
aboard skill install --force
# 或只升级一个
aboard skill install --force management
aboard skill install --force workflow
```

普通 `skill install` 会补齐缺少的 Skill，并保留内容不同的已安装版本。

## 迁移仓库内的 `.agent-board.json`

项目身份现在保存在本机 Git common dir（`.git/agent-board.json`），不再放在工作树里。仍只有旧版仓库内 `.agent-board.json`（version 1 或 2）的项目，按本机是否已有对应 Board 数据区分：

- **本机已有 `~/.agent-board/<project_id>/board.db`**：普通命令返回 `PROJECT_IDENTITY_MIGRATION_REQUIRED`，需要显式迁移（见下）。
- **本机没有对应 Board 数据**（例如另一台机器上的新 clone）：视为 fresh clone，普通命令返回 `PROJECT_NOT_FOUND`。运行 `aboard init` 生成新的本地身份和新的 `project_id`，不继承旧 ID，也不修改旧文件。

普通 `aboard doctor` 是只读的；本机已有 Board 数据时，根据提示显式迁移：

```bash
aboard doctor --fix
# 可覆盖项目名（version 2 默认沿用原名，version 1 默认用目录名）
aboard doctor --fix --name "Project Name"
```

迁移保持原 `project_id` 不变，继续使用原 `~/.agent-board/<project_id>/board.db`，不移动、不修改 Board 数据，也不修改或删除仓库里的 `.agent-board.json`。每台已有 Board 数据的机器各自执行一次迁移；全部迁移后，可用 `git rm .agent-board.json` 移除旧文件。

## 清理当前项目

```bash
aboard clean
# 自动化场景显式跳过确认
aboard clean --yes
```

`clean` 只删除当前项目的本地身份（`.git/agent-board.json`）和对应 `~/.agent-board/<project_id>/` 数据，不删除 binary、Skills、其他项目数据，也不删除仓库里遗留的 `.agent-board.json`。

## 卸载

```bash
aboard uninstall
aboard uninstall --yes
```

卸载目标包括当前用户的 `aboard` binary、Agent Board 管理的 Skills，以及 `~/.agent-board/` 下的本地 Board 数据。

内容被修改过的 managed Skill 默认保留；确认要删除这些内容时：

```bash
aboard uninstall --force
```

`--force` 只授权删除 modified managed Skills，不等于 `--yes`。非 Agent Board 管理的 Skill 和不匹配的 legacy Skill 始终保留并报告。
