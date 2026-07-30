package cmd

import (
	"context"
	"testing"

	"github.com/futuretea/mysql-mcp-server/pkg/core/config"
	"github.com/futuretea/mysql-mcp-server/pkg/toolset"
)

func TestBuildRuntimeToolsetsUsesUnavailableServiceForInvalidMySQLRuntimeConfig(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "missing username",
			env: map[string]string{
				"MYSQL_MCP_HOST":    "127.0.0.1",
				"MYSQL_MCP_DB_PORT": "3306",
			},
		},
		{
			name: "invalid database port",
			env: map[string]string{
				"MYSQL_MCP_HOST":     "127.0.0.1",
				"MYSQL_MCP_DB_PORT":  "0",
				"MYSQL_MCP_USERNAME": "reader",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MYSQL_DSN", "reader@tcp(127.0.0.1:1)/inventory")
			t.Setenv("MYSQL_MCP_HOST", "")
			t.Setenv("MYSQL_MCP_DB_PORT", "")
			t.Setenv("MYSQL_MCP_USERNAME", "")
			t.Setenv("MYSQL_MCP_PASSWORD", "")
			t.Setenv("MYSQL_MCP_DATABASE", "")
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			toolsets, release, runtimeErr := buildRuntimeToolsets()
			defer release()
			if runtimeErr == nil {
				t.Fatal("buildRuntimeToolsets() error = nil, want invalid configuration")
			}

			tools, err := buildToolCatalogForToolsets(&config.StaticConfig{}, toolsets)
			if err != nil {
				t.Fatalf("buildToolCatalogForToolsets: %v", err)
			}
			assertMySQLRuntimeTools(t, tools)

			tool, ok := findTool(tools, "mysql_get_server_info")
			if !ok {
				t.Fatal("mysql_get_server_info is unavailable")
			}
			_, err = tool.Handler(context.Background(), map[string]any{})
			if got, want := errorString(err), "[mysql_configuration_unavailable] configuration unavailable"; got != want {
				t.Fatalf("mysql_get_server_info error = %q, want %q", got, want)
			}
		})
	}
}

func assertMySQLRuntimeTools(t *testing.T, tools []toolset.ServerTool) {
	t.Helper()
	got := make(map[string]bool, len(tools))
	for _, tool := range tools {
		got[tool.Tool.Name] = true
	}
	for _, name := range []string{
		"mysql_get_server_info",
		"mysql_list_databases",
		"mysql_list_tables",
		"mysql_describe_table",
		"mysql_list_indexes",
		"mysql_query",
	} {
		if !got[name] {
			t.Fatalf("runtime tool catalog missing %q: %v", name, got)
		}
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
