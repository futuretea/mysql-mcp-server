package toolcatalog

import (
	"fmt"

	"example.invalid/mcp-template-module-placeholder/pkg/toolset"
)

// Build collects, filters, and validates tools for internal consumers.
func Build(toolsets []toolset.Toolset, opts toolset.FilterOptions) ([]toolset.ServerTool, error) {
	catalog := make([]toolset.ServerTool, 0)
	for _, configuredToolset := range toolsets {
		if configuredToolset == nil {
			continue
		}
		catalog = append(catalog, configuredToolset.GetTools()...)
	}

	filtered := toolset.FilterTools(catalog, opts)
	seen := make(map[string]struct{}, len(filtered))
	for _, tool := range filtered {
		name := tool.Tool.Name
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate tool name %q", name)
		}
		seen[name] = struct{}{}
	}
	return filtered, nil
}
