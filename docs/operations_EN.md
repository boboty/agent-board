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

## Migrating a repository-tracked `.agent-board.json`

The project identity now lives in this machine's Git common directory (`.git/agent-board.json`), not in the worktree. A project that still has only the legacy repository-tracked `.agent-board.json` (version 1 or 2) is handled according to whether this machine already has its Board data:

- **`~/.agent-board/<project_id>/board.db` exists on this machine**: normal commands fail with `PROJECT_IDENTITY_MIGRATION_REQUIRED` and need an explicit migration (below).
- **No Board data on this machine** (for example a fresh clone on another machine): it is treated as a fresh clone and normal commands fail with `PROJECT_NOT_FOUND`. Run `aboard init` to create a new local identity with a new `project_id`; the old ID is not inherited and the old file is not modified.

Regular `aboard doctor` is read-only. When this machine has the Board data, migrate explicitly:

```bash
aboard doctor --fix
# Override the project name (version 2 keeps its name, version 1 defaults to the directory name)
aboard doctor --fix --name "Project Name"
```

Migration preserves the existing `project_id` and keeps using `~/.agent-board/<project_id>/board.db`. It does not move or modify Board data, and it does not modify or remove the repository's `.agent-board.json`. Run the migration once on each machine that has Board data; after all have migrated, remove the old file with `git rm .agent-board.json`.

## Clean the current project

```bash
aboard clean
# explicitly skip confirmation in automation
aboard clean --yes
```

`clean` removes only the current project's local identity (`.git/agent-board.json`) and matching `~/.agent-board/<project_id>/` data. It does not remove the binary, Skills, any other project's data, or a leftover repository-tracked `.agent-board.json`.

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
