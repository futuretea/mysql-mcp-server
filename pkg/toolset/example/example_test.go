package example_test

import (
	"context"
	"strings"
	"testing"

	"example.invalid/mcp-template-module-placeholder/pkg/toolset"
	"example.invalid/mcp-template-module-placeholder/pkg/toolset/example"
)

func TestExampleTools(t *testing.T) {
	tools := (&example.Toolset{}).GetTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	handlers := map[string]toolset.ToolHandler{}
	for _, tool := range tools {
		handlers[tool.Tool.Name] = tool.Handler
	}
	if handlers["echo"] == nil || handlers["ping"] == nil {
		t.Fatal("missing echo or ping handler")
	}

	got, err := handlers["echo"](context.Background(), map[string]any{"message": " hi "})
	if err != nil || got != "hi" {
		t.Fatalf("echo = %q, %v", got, err)
	}

	_, err = handlers["echo"](context.Background(), map[string]any{"message": "   "})
	if err == nil || !strings.Contains(err.Error(), "message is required") {
		t.Fatalf("expected message required error, got %v", err)
	}

	pong, err := handlers["ping"](context.Background(), nil)
	if err != nil || !strings.HasPrefix(pong, "pong ") {
		t.Fatalf("ping = %q, %v", pong, err)
	}
}
