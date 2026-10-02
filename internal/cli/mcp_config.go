package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func runMCPConfig(args []string, env Env) error {
	if len(args) > 1 {
		return usagef("mcp config: expected at most one harness")
	}
	harness := "generic"
	if len(args) == 1 {
		harness = args[0]
	}
	if harness != "generic" && harness != "claude-code" && harness != "codex" && harness != "opencode" {
		return usagef("mcp config: unknown harness %q (choose generic, claude-code, codex, or opencode)", harness)
	}

	resolve := env.ExecutablePath
	if resolve == nil {
		resolve = runningExecutable
	}
	executable, err := resolve()
	if err != nil {
		return fmt.Errorf("resolve aboard executable: %w", err)
	}
	if !filepath.IsAbs(executable) {
		return fmt.Errorf("resolve aboard executable: path is not absolute: %q", executable)
	}
	actor := "harness/" + harness

	switch harness {
	case "generic":
		return writeJSON(env.Stdout, map[string]any{
			"command": executable,
			"args":    []string{"mcp"},
			"env":     map[string]string{"AGENT_BOARD_ACTOR": actor},
		})
	case "claude-code":
		fmt.Fprintln(env.Stdout, "Claude Code — run this command (user scope):")
		_, err = fmt.Fprintf(env.Stdout, "claude mcp add --transport stdio --scope user aboard --env %s -- %s mcp\n", shellQuote("AGENT_BOARD_ACTOR="+actor), shellQuote(executable))
		return err
	case "codex":
		fmt.Fprintln(env.Stdout, "Codex — add this to ~/.codex/config.toml:")
		_, err = fmt.Fprintf(env.Stdout, "\n[mcp_servers.aboard]\ncommand = %s\nargs = [\"mcp\"]\nenv = { AGENT_BOARD_ACTOR = %s }\n", strconv.Quote(executable), strconv.Quote(actor))
		return err
	case "opencode":
		fmt.Fprintln(env.Stdout, "OpenCode — merge this into ~/.config/opencode/opencode.json:")
		_, err = fmt.Fprintf(env.Stdout, "\n{\n  \"mcp\": {\n    \"aboard\": {\n      \"type\": \"local\",\n      \"command\": [%s, \"mcp\"],\n      \"environment\": {\"AGENT_BOARD_ACTOR\": %s},\n      \"enabled\": true\n    }\n  }\n}\n", jsonString(executable), jsonString(actor))
		return err
	default:
		panic("validated harness fell through")
	}
}

func runningExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
