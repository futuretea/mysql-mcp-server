package example

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"example.invalid/mcp-template-module-placeholder/pkg/toolset"
)

const Domain = "example"

// Toolset exposes sample tools for the template.
type Toolset struct{}

func (t *Toolset) GetName() string {
	return "example"
}

func (t *Toolset) GetDescription() string {
	return "Example tools that demonstrate how to extend this MCP server template"
}

func (t *Toolset) GetTools() []toolset.ServerTool {
	return []toolset.ServerTool{
		{
			Tool: mcp.NewTool("echo",
				mcp.WithDescription("Echo back the provided message"),
				mcp.WithString("message",
					mcp.Required(),
					mcp.Description("Message to echo"),
				),
			),
			Handler: echoHandler,
			Domain:  Domain,
		},
		{
			Tool: mcp.NewTool("ping",
				mcp.WithDescription("Return a simple pong response with the current UTC time"),
			),
			Handler: pingHandler,
			Domain:  Domain,
		},
	}
}

func echoHandler(_ context.Context, params map[string]any) (string, error) {
	message, _ := params["message"].(string)
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("message is required")
	}
	return message, nil
}

func pingHandler(_ context.Context, _ map[string]any) (string, error) {
	return fmt.Sprintf("pong %s", time.Now().UTC().Format(time.RFC3339)), nil
}
