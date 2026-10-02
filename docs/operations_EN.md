# Agent Board operations

## Upgrade

```bash
go install github.com/boboty/agent-board/cmd/aboard@latest
aboard version
aboard doctor
```

If `doctor` reports installed Skill content that differs from the current binary's embedded version, replace it explicitly:

```bash
aboard skill install --force
# or only one Skill
aboard skill install --force management
aboard skill install --force workflow
```

Regular `skill install` adds missing Skills and preserves installed content that differs.

## Project Identity v1 → v2

Regular `aboard doctor` is read-only. When it detects an old identity, migrate explicitly:

```bash
aboard doctor --fix
# override the inferred project name if needed
aboard doctor --fix --name "Project Name"
```

Migration preserves the existing `project_id`, does not move Board data, and only upgrades `.agent-board.json` in the repository.

## Clean the current project

```bash
aboard clean
# explicitly skip confirmation in automation
aboard clean --yes
```

`clean` removes only the current project's `.agent-board.json` and matching `~/.agent-board/<project_id>/` data. It does not remove the binary, Skills, or any other project's data.

## Uninstall

```bash
aboard uninstall
aboard uninstall --yes
```

Uninstall targets the current user's `aboard` binary, Agent Board-managed Skills, and local Board data under `~/.agent-board/`.

Modified managed Skills are preserved by default. To explicitly authorize deleting them:

```bash
aboard uninstall --force
```

`--force` only authorizes removal of modified managed Skills; it does not imply `--yes`. Unmanaged and content-mismatched legacy Skills are preserved and reported for manual handling.
