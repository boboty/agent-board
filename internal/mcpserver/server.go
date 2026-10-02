// Package mcpserver exposes the Board operations as MCP tools. It is a pure
// transport adapter: every tool is generated from the ops catalog and every
// call is dispatched through ops.Service.Call.
package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/boboty/agent-board/internal/ops"
)

// Info identifies the server and the Board it serves.
type Info struct {
	Version      string
	ProjectID    string
	DatabasePath string
}

// New builds an MCP server with one tool per Board operation.
func New(service *ops.Service, info Info) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "aboard", Version: info.Version},
		&mcp.ServerOptions{Instructions: instructions(info)},
	)
	closedWorld := false
	for _, op := range ops.Operations() {
		tool := &mcp.Tool{
			Name:        op.Name,
			Description: op.Description,
			InputSchema: op.InputSchema,
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: op.ReadOnly, OpenWorldHint: &closedWorld},
		}
		name := op.Name
		server.AddTool(tool, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result, err := service.Call(ctx, name, request.Params.Arguments)
			if err != nil {
				return toolResult(map[string]any{"error": ops.DescribeError(err)}, true), nil
			}
			return toolResult(result, false), nil
		})
	}
	return server
}

// toolResult returns value as structured content plus the same JSON as text
// for clients that read only text content.
func toolResult(value any, isError bool) *mcp.CallToolResult {
	text, err := json.Marshal(value)
	if err != nil {
		value = map[string]any{"error": ops.DescribeError(err)}
		text, _ = json.Marshal(value)
		isError = true
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(text)}},
		StructuredContent: value,
		IsError:           isError,
	}
}

func instructions(info Info) string {
	return "Agent Board shared task ledger for project " + info.ProjectID + " (database " + info.DatabasePath + ").\n" +
		"The tools record Board facts: tasks, explicit task-level state (READY, IN_PROGRESS, DONE, BLOCKED), READY ordering, task facts, and the audit log. " +
		"They do not schedule work or decide workflow; which role should call which tool, and when, is defined by the Agent Board Workflow Skill.\n" +
		"Mutations of a task require expected_version (the version you last read) and fail with VERSION_CONFLICT when it is stale; re-read the task before retrying. " +
		"Pass idempotency_key to make a retried mutation safe. Failed calls return isError with {\"error\": {code, message, details, retryable}}."
}
