package toolset

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// Toolset defines a group of MCP tools.
type Toolset interface {
	GetName() string
	GetDescription() string
	GetTools() []ServerTool
}

// ServerTool combines an MCP tool definition with its handler.
type ServerTool struct {
	Tool    mcp.Tool
	Handler ToolHandler
	Domain  string
}

// ToolHandler handles a tool call.
type ToolHandler func(ctx context.Context, params map[string]any) (string, error)

// FilterOptions controls which tools are exposed.
type FilterOptions struct {
	EnabledTools    []string
	DisabledTools   []string
	EnabledDomains  []string
	DisabledDomains []string
}

// FilterTools applies enable/disable lists for tools and domains.
func FilterTools(tools []ServerTool, opts FilterOptions) []ServerTool {
	enabledTools := toSet(opts.EnabledTools)
	disabledTools := toSet(opts.DisabledTools)
	enabledDomains := toSet(opts.EnabledDomains)
	disabledDomains := toSet(opts.DisabledDomains)

	filtered := make([]ServerTool, 0, len(tools))
	for _, tool := range tools {
		name := tool.Tool.Name
		domain := tool.Domain

		if len(enabledTools) > 0 {
			if _, ok := enabledTools[name]; !ok {
				continue
			}
		}
		if _, ok := disabledTools[name]; ok {
			continue
		}
		if len(enabledDomains) > 0 {
			if _, ok := enabledDomains[domain]; !ok {
				continue
			}
		}
		if _, ok := disabledDomains[domain]; ok {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func toSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}
