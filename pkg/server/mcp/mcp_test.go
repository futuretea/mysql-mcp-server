package mcp_test

import (
	"strings"
	"testing"

	internalmysql "github.com/futuretea/mysql-mcp-server/internal/mysql"
	"github.com/futuretea/mysql-mcp-server/pkg/core/config"
	mcpserver "github.com/futuretea/mysql-mcp-server/pkg/server/mcp"
	"github.com/futuretea/mysql-mcp-server/pkg/toolset"
	toolmysql "github.com/futuretea/mysql-mcp-server/pkg/toolset/mysql"
)

func TestNewServerRegistersMySQLTools(t *testing.T) {
	server, err := mcpserver.NewServer(mcpserver.Configuration{
		StaticConfig: &config.StaticConfig{LogLevel: "info"},
		Toolsets:     []toolset.Toolset{toolmysql.New(internalmysql.NewUnavailableService())},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	if !server.IsHealthy() {
		t.Fatal("expected healthy server")
	}
	tools := server.GetEnabledTools()
	if len(tools) != 6 {
		t.Fatalf("expected 6 tools, got %v", tools)
	}
	names := map[string]bool{}
	for _, name := range tools {
		names[name] = true
	}
	for _, name := range []string{"mysql_get_server_info", "mysql_list_databases", "mysql_list_tables", "mysql_describe_table", "mysql_list_indexes", "mysql_query"} {
		if !names[name] {
			t.Fatalf("expected %q, got %v", name, tools)
		}
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
		Toolsets: []toolset.Toolset{toolmysql.New(internalmysql.NewUnavailableService())},
	})
	if err == nil || !strings.Contains(err.Error(), "no tools registered") {
		t.Fatalf("expected no tools registered error, got %v", err)
	}
}

func TestNewServerRejectsDuplicateToolNames(t *testing.T) {
	_, err := mcpserver.NewServer(mcpserver.Configuration{
		StaticConfig: &config.StaticConfig{LogLevel: "info"},
		Toolsets: []toolset.Toolset{
			toolmysql.New(internalmysql.NewUnavailableService()),
			toolmysql.New(internalmysql.NewUnavailableService()),
		},
	})
	if err == nil || !strings.Contains(err.Error(), `duplicate tool name "mysql_get_server_info"`) {
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
