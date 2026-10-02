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

## Project Identity v1 → v2

普通 `aboard doctor` 是只读的。发现旧 identity 时，根据提示显式迁移：

```bash
aboard doctor --fix
# 可覆盖自动推导的项目名
aboard doctor --fix --name "Project Name"
```

迁移保持原 `project_id` 不变，不移动 Board 数据，只升级仓库里的 `.agent-board.json`。

## 清理当前项目

```bash
aboard clean
# 自动化场景显式跳过确认
aboard clean --yes
```

`clean` 只删除当前项目的 `.agent-board.json` 和对应 `~/.agent-board/<project_id>/` 数据，不删除 binary、Skills 或其他项目数据。

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
