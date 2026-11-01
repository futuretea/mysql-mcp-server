package cmd_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/futuretea/mysql-mcp-server/internal/cmd"
)

func TestRootHelpListsSubcommands(t *testing.T) {
	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	text := out.String()
	for _, want := range []string{"mcp", "tools", "version", "completion"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %q:\n%s", want, text)
		}
	}
}

func TestRootWithoutArgsShowsHelp(t *testing.T) {
	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{})
	if err := root.Execute(); err != nil {
		t.Fatalf("root with no args: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "Available Commands") {
		t.Fatalf("expected help output, got:\n%s", text)
	}
	if strings.Contains(text, "registered MCP tools") || strings.Contains(text, "starting HTTP MCP server") {
		t.Fatalf("root must not start the MCP server:\n%s", text)
	}
}

func TestToolsListAndCall(t *testing.T) {
	clearMySQLMCPStaticConfigEnv(t)

	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"tools", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("tools list: %v", err)
	}
	for _, name := range []string{"mysql_get_server_info", "mysql_list_databases", "mysql_list_tables", "mysql_describe_table", "mysql_list_indexes", "mysql_query"} {
		if !strings.Contains(out.String(), name) {
			t.Fatalf("tools list missing %q:\n%s", name, out.String())
		}
	}
	if strings.Contains(out.String(), "echo") || strings.Contains(out.String(), "ping") {
		t.Fatalf("tools list still exposes example tools:\n%s", out.String())
	}

	out.Reset()
	root = cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"tools", "call", "mysql_query", "--params", `{}`})
	err := root.Execute()
	if err == nil || err.Error() != "[mysql_invalid_params] invalid parameters" {
		t.Fatalf("tools call error = %v, want mysql_invalid_params", err)
	}
}

func TestToolsCallReportsMissingMySQLUsername(t *testing.T) {
	clearMySQLMCPStaticConfigEnv(t)
	t.Setenv("MYSQL_DSN", "reader@tcp(127.0.0.1:1)/inventory")
	t.Setenv("MYSQL_MCP_HOST", "127.0.0.1")
	t.Setenv("MYSQL_MCP_DB_PORT", "3306")
	t.Setenv("MYSQL_MCP_USERNAME", "")
	t.Setenv("MYSQL_MCP_PASSWORD", "")
	t.Setenv("MYSQL_MCP_DATABASE", "")

	var out bytes.Buffer
	var errOut bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &errOut})
	root.SetArgs([]string{"tools", "call", "mysql_get_server_info"})

	err := root.Execute()
	if err == nil || err.Error() != "[mysql_configuration_unavailable] configuration unavailable" {
		t.Fatalf("Execute() error = %v, want configuration unavailable", err)
	}
	if got, want := errOut.String(), "mysql runtime configuration unavailable: MYSQL_MCP_USERNAME is required\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func clearMySQLMCPStaticConfigEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"MYSQL_MCP_PORT",
		"MYSQL_MCP_LISTEN",
		"MYSQL_MCP_SSE_BASE_URL",
		"MYSQL_MCP_LOG_LEVEL",
		"MYSQL_MCP_ENABLED_TOOLS",
		"MYSQL_MCP_DISABLED_TOOLS",
		"MYSQL_MCP_ENABLED_DOMAINS",
		"MYSQL_MCP_DISABLED_DOMAINS",
		"MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE",
	} {
		t.Setenv(key, "")
	}
}

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.Contains(out.String(), "mysql-mcp-server") {
		t.Fatalf("unexpected version output: %q", out.String())
	}
}

func TestToolsUnknownTool(t *testing.T) {
	clearMySQLMCPStaticConfigEnv(t)

	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"tools", "describe", "missing-tool"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown tool error, got %v", err)
	}

	root = cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"tools", "call", "missing-tool", "--params", "{}"})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown tool error on call, got %v", err)
	}
}

func TestToolsRejectsNullParams(t *testing.T) {
	clearMySQLMCPStaticConfigEnv(t)

	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"tools", "call", "mysql_get_server_info", "--params", "null"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "expected an object") {
		t.Fatalf("expected JSON object error, got %v", err)
	}
}

func TestCommandsReturnOutputErrors(t *testing.T) {
	clearMySQLMCPStaticConfigEnv(t)

	writeErr := errors.New("write failed")
	tests := []struct {
		name string
		args []string
	}{
		{name: "completion script", args: []string{"completion", "bash"}},
		{name: "completion install", args: []string{"completion", "install"}},
		{name: "version", args: []string{"version"}},
		{name: "tools list", args: []string{"tools", "list"}},
		{name: "tools describe", args: []string{"tools", "describe", "mysql_get_server_info"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := cmd.NewRootCommand(cmd.IOStreams{
				In:     strings.NewReader(""),
				Out:    errorWriter{err: writeErr},
				ErrOut: io.Discard,
			})
			root.SetArgs(tt.args)
			if err := root.Execute(); !errors.Is(err, writeErr) {
				t.Fatalf("Execute() error = %v, want %v", err, writeErr)
			}
		})
	}
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}
