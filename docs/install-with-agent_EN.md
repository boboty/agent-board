# Install Agent Board with your agent

Give the prompt below to Codex, Claude Code, or OpenCode. The agent should choose a route from the actual environment and must not count unverified steps as successful.

## Copyable installation prompt

```text
Follow https://github.com/boboty/agent-board/blob/main/docs/install-with-agent_EN.md to install and verify Agent Board in this environment.

1. First confirm the current OS and architecture, the harness running this session, and the Git repository I want initialized. Identify the harness from the current session/app; confirm only this environment and do not scan unrelated harnesses or repositories. If the target repository is unclear, ask me for its path rather than guessing.
2. Check whether `aboard` is already installed and runnable. If not, choose an available prebuilt release for this OS and architecture; if Go 1.25 or newer is already available, `go install github.com/boboty/agent-board/cmd/aboard@latest` is also an option. Put the binary in an existing PATH directory. If PATH must change, edit only the necessary setting for the current user's shell and report the exact file changed. If neither route is available, stop and explain the missing prerequisite and next step.
3. Run `aboard version` and confirm the actual executable path and version (`command -v aboard`, or the current Shell's equivalent on Windows). If a new Shell is needed to pick up PATH, say so; do not call CLI installation complete until it is verified.
4. Run `aboard skill install` and `aboard skill check`. These install `agent-board-management` and `agent-board-workflow` in the supported user-level locations: Codex uses `~/.agents/skills/`; Claude Code and OpenCode share `~/.claude/skills/`. By default, installation adds missing Skills and preserves files with different content. Report each `different` path and do not use `--force`. Only if I explicitly authorize overwriting the corresponding Skill, run `aboard skill install --force management` or `aboard skill install --force workflow`, then check again.
5. In the Git repository I specified, run `aboard init`, then `aboard doctor`. `init` stores the local project identity in that repository's Git common directory and creates Board data in the user data directory; neither is a file in the repository worktree. If the target is not a Git repository, is not writable, or doctor fails, stop and report the exact reason and next step. Do not clean or overwrite existing project data.
6. Report: confirmed OS/architecture and current harness; aboard version and binary path; `current`, `missing`, or `different` status of both Skills at each supported location; target repository and `init` result; full `doctor` result; unresolved problems and next steps. Call the setup “installed and ready” only if all required checks succeeded and the target project was initialized. Installation does not define, queue, or execute a Task and does not authorize commit, push, merge, or release.

After installation, tell me how to define my first Task and point me to https://github.com/boboty/agent-board/blob/main/docs/first-task_EN.md. This CLI path does not require MCP, Paseo, Orca, or another scheduler; MCP is optional.
```

## How to judge the result

`aboard doctor` checks the binary, Skills, and current project configuration. Exit code `0` means it found no problems; `1` means a component needs attention; `2` means invalid command usage. Its Skill status lists Claude Code, Codex, and OpenCode targets. In the current implementation, Claude Code and OpenCode point to the same `~/.claude/skills` files. Doctor is a local diagnostic; it does not install missing components.

Current release builds cover macOS (Apple Silicon and Intel), Linux (amd64 and arm64), and Windows (amd64). If there is no release binary for the current OS/architecture, use Go installation only when Go 1.25+ is available. Otherwise report the unsupported combination and next step without claiming installation succeeded. After Go installation, verify that `aboard` is actually on the current Shell's PATH.

The harness paths follow the current CLI Skill targets: Codex uses `~/.agents/skills/agent-board-<name>/SKILL.md`; Claude Code and OpenCode share `~/.claude/skills/agent-board-<name>/SKILL.md`. `aboard skill check` and `doctor` inspect file contents only; they do not launch a harness to confirm it loaded a Skill. This guide has not tested actual discovery/loading in all three harnesses. If the current session cannot see a Skill, report “files installed; harness loading unverified” and suggest reopening the harness or checking its Skill discovery instructions.

For older or different Skill copies, `aboard skill check` shows status and path. Regular `skill install` preserves content that differs. `--force` is explicit overwrite authorization, not a routine repair option. See [Operations](operations_EN.md).
## After installation

1. In the target repository, run `aboard board` or `aboard web` to confirm the Board is readable.
2. Ask your current agent to draft the first Task with you using the `agent-board-management` Skill. Once you approve the complete definition, explicitly authorize queueing it as `READY`.
3. Follow [Complete your first Task](first-task_EN.md) for one Worker → Independent Verifier → DONE cycle.

See [Everyday use](usage_EN.md) for common commands. The [Workflow Skill](../workflow/SKILL.md) and [Management Skill](../management/SKILL.md) define the full workflow rules.
