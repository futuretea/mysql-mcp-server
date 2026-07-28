package toolcatalog_test

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"example.invalid/mcp-template-module-placeholder/internal/toolcatalog"
	"example.invalid/mcp-template-module-placeholder/pkg/toolset"
)

func TestBuildRejectsVisibleDuplicateNames(t *testing.T) {
	toolsets := []toolset.Toolset{
		staticToolset{tools: []toolset.ServerTool{{Tool: mcp.NewTool("duplicate"), Domain: "one"}}},
		staticToolset{tools: []toolset.ServerTool{{Tool: mcp.NewTool("duplicate"), Domain: "two"}}},
	}

	if _, err := toolcatalog.Build(toolsets, toolset.FilterOptions{}); err == nil {
		t.Fatal("Build() error = nil, want duplicate tool name error")
	}

	catalog, err := toolcatalog.Build(toolsets, toolset.FilterOptions{EnabledDomains: []string{"one"}})
	if err != nil {
		t.Fatalf("Build() with one visible domain: %v", err)
	}
	if len(catalog) != 1 || catalog[0].Domain != "one" {
		t.Fatalf("Build() = %+v, want only domain one", catalog)
	}
}

type staticToolset struct {
	tools []toolset.ServerTool
}

func (t staticToolset) GetName() string        { return "test" }
func (t staticToolset) GetDescription() string { return "test toolset" }
func (t staticToolset) GetTools() []toolset.ServerTool {
	return t.tools
}
