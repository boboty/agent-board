// Command agent-board is the Agent Board CLI and MCP server.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/boboty/agent-board/internal/cli"
	"github.com/boboty/agent-board/internal/workspace"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	code := cli.Run(ctx, os.Args[1:], cli.Env{
		Dir:     dir,
		Home:    workspace.HomeDir(),
		Actor:   os.Getenv("AGENT_BOARD_ACTOR"),
		Version: version,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	})
	stop()
	os.Exit(code)
}
