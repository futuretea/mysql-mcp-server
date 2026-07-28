package mcp_test

import (
	"strings"
	"testing"

	"example.invalid/mcp-template-module-placeholder/pkg/core/config"
	mcpserver "example.invalid/mcp-template-module-placeholder/pkg/server/mcp"
	"example.invalid/mcp-template-module-placeholder/pkg/toolset"
	"example.invalid/mcp-template-module-placeholder/pkg/toolset/example"
)

func TestNewServerRegistersExampleTools(t *testing.T) {
	server, err := mcpserver.NewServer(mcpserver.Configuration{
		StaticConfig: &config.StaticConfig{LogLevel: "info"},
		Toolsets:     []toolset.Toolset{&example.Toolset{}},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	if !server.IsHealthy() {
		t.Fatal("expected healthy server")
	}
	tools := server.GetEnabledTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %v", tools)
	}
	names := map[string]bool{}
	for _, name := range tools {
		names[name] = true
	}
	if !names["echo"] || !names["ping"] {
		t.Fatalf("expected echo and ping, got %v", tools)
	}
}

func TestNewServerRequiresStaticConfig(t *testing.T) {
	_, err := mcpserver.NewServer(mcpserver.Configuration{})
	if err == nil || !strings.Contains(err.Error(), "static config is required") {
		t.Fatalf("expected static config error, got %v", err)
	}
}

func TestNewServerRejectsEmptyToolFilter(t *testing.T) {
	_, err := mcpserver.NewServer(mcpserver.Configuration{
		StaticConfig: &config.StaticConfig{
			LogLevel:     "info",
			EnabledTools: []string{"missing"},
		},
		Toolsets: []toolset.Toolset{&example.Toolset{}},
	})
	if err == nil || !strings.Contains(err.Error(), "no tools registered") {
		t.Fatalf("expected no tools registered error, got %v", err)
	}
}

func TestNewServerRejectsDuplicateToolNames(t *testing.T) {
	_, err := mcpserver.NewServer(mcpserver.Configuration{
		StaticConfig: &config.StaticConfig{LogLevel: "info"},
		Toolsets: []toolset.Toolset{
			&example.Toolset{},
			&example.Toolset{},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `duplicate tool name "echo"`) {
		t.Fatalf("expected duplicate tool name error, got %v", err)
	}
}

func TestNewTextResult(t *testing.T) {
	ok := mcpserver.NewTextResult("hello", nil)
	if ok.IsError || len(ok.Content) != 1 {
		t.Fatalf("unexpected ok result: %+v", ok)
	}

	fail := mcpserver.NewTextResult("", errString("boom"))
	if !fail.IsError {
		t.Fatal("expected error result")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
