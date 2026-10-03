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

## Project identity

The project identity is `agent-board.json` in this machine's Git common directory (`.git/agent-board.json` in a normal clone), outside the worktree and never committed; Board data lives in `~/.agent-board/<project_id>/board.db`. All worktrees of one local repository share the identity and Board; each independent clone runs its own `aboard init`.

## Clean the current project

```bash
aboard clean
# explicitly skip confirmation in automation
aboard clean --yes
```

`clean` removes only the current project's local identity (`.git/agent-board.json`) and matching `~/.agent-board/<project_id>/` data. It does not remove the binary, Skills, or any other project's data.

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
