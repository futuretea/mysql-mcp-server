package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/futuretea/mysql-mcp-server/internal/cmd"
)

func TestBlankFilterListsDoNotExposeRestrictedTools(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	var accepts atomic.Int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepts.Add(1)
			_ = conn.Close()
		}
	}()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		yaml    string
		envKey  string
		envVal  string
		args    []string
		field   string
		hidden  string
		withMCP bool
	}{
		{
			name:   "enabled tools env spaces",
			yaml:   "enabled_tools: [mysql_query]\n",
			envKey: "MYSQL_MCP_ENABLED_TOOLS",
			envVal: " ",
			field:  "enabled_tools",
			hidden: "mysql_get_server_info",
		},
		{
			name:   "disabled tools env spaces override yaml",
			yaml:   "disabled_tools: [mysql_query]\n",
			envKey: "MYSQL_MCP_DISABLED_TOOLS",
			envVal: " , ",
			field:  "disabled_tools",
			hidden: "mysql_query",
		},
		{
			name:   "enabled domains env tab",
			yaml:   "enabled_domains: [other]\n",
			envKey: "MYSQL_MCP_ENABLED_DOMAINS",
			envVal: "\t",
			field:  "enabled_domains",
			hidden: "mysql_get_server_info",
		},
		{
			name:    "disabled domains env spaces override yaml",
			yaml:    "disabled_domains: [mysql]\n",
			envKey:  "MYSQL_MCP_DISABLED_DOMAINS",
			envVal:  " ",
			field:   "disabled_domains",
			hidden:  "mysql_get_server_info",
			withMCP: true,
		},
		{
			name:   "disabled tools flag spaces",
			yaml:   "disabled_tools: [mysql_query]\n",
			args:   []string{"--disabled-tools", " "},
			field:  "disabled_tools",
			hidden: "mysql_query",
		},
		{
			name:   "enabled tools flag commas",
			yaml:   "enabled_tools: [mysql_query]\n",
			args:   []string{"--enabled-tools", ","},
			field:  "enabled_tools",
			hidden: "mysql_get_server_info",
		},
		{
			name:   "yaml blank enabled tools",
			yaml:   "enabled_tools: [\" \", \"\"]\n",
			field:  "enabled_tools",
			hidden: "mysql_query",
		},
		{
			name:   "yaml blank disabled domains",
			yaml:   "disabled_domains: [\" \"]\n",
			field:  "disabled_domains",
			hidden: "mysql_get_server_info",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			clearMySQLMCPStaticConfigEnv(t)
			t.Setenv("MYSQL_MCP_HOST", "127.0.0.1")
			t.Setenv("MYSQL_MCP_DB_PORT", port)
			t.Setenv("MYSQL_MCP_USERNAME", "reader")
			t.Setenv("MYSQL_MCP_PASSWORD", "")
			t.Setenv("MYSQL_MCP_DATABASE", "")
			if tt.envKey != "" {
				t.Setenv(tt.envKey, tt.envVal)
			}
			path := writeConfig(t, tt.yaml)
			before := accepts.Load()

			listArgs := append([]string{"tools", "list", "--json", "--config", path}, tt.args...)
			listOut, listErr := runRoot(t, listArgs)
			callArgs := append([]string{"tools", "call", tt.hidden, "--params", "{}", "--config", path}, tt.args...)
			callOut, callErr := runRoot(t, callArgs)
			if tt.withMCP {
				mcpArgs := append([]string{"mcp", "--config", path}, tt.args...)
				_, mcpErr := runRootContext(t, time.Second, mcpArgs)
				if mcpErr == nil || !strings.Contains(mcpErr.Error(), tt.field+" contains only blank names") {
					t.Fatalf("mcp error = %v, want blank %s", mcpErr, tt.field)
				}
			}

			if got := accepts.Load() - before; got != 0 {
				t.Fatalf("MySQL accepts = %d, want 0", got)
			}
			for _, err := range []error{listErr, callErr} {
				if err == nil || !strings.Contains(err.Error(), tt.field+" contains only blank names") {
					t.Fatalf("error = %v, want blank %s", err, tt.field)
				}
				if strings.Contains(err.Error(), "configuration unavailable") || strings.Contains(err.Error(), "connection failed") {
					t.Fatalf("error reached MySQL: %v", err)
				}
			}
			if strings.Contains(listOut.out, tt.hidden) || strings.Contains(callOut.out, tt.hidden) {
				t.Fatalf("restricted tool %s leaked in output", tt.hidden)
			}
		})
	}
}

func TestFilterListPrecedenceKeepsLegalEmptyValues(t *testing.T) {
	path := writeConfig(t, "enabled_tools: []\ndisabled_tools: []\nenabled_domains: []\ndisabled_domains: []\n")
	clearMySQLMCPStaticConfigEnv(t)
	out, err := runRoot(t, []string{"tools", "list", "--json", "--config", path})
	if err != nil {
		t.Fatalf("empty yaml lists: %v", err)
	}
	if got := listNames(out); len(got) != 6 {
		t.Fatalf("empty yaml lists = %v, want 6 tools", got)
	}

	clearMySQLMCPStaticConfigEnv(t)
	denyPath := writeConfig(t, "disabled_tools: [mysql_query]\n")
	t.Setenv("MYSQL_MCP_DISABLED_TOOLS", "")
	out, err = runRoot(t, []string{"tools", "list", "--json", "--config", denyPath})
	if err != nil {
		t.Fatalf("empty env list: %v", err)
	}
	if names := listNames(out); containsName(names, "mysql_query") {
		t.Fatalf("empty env cleared YAML denylist: %v", names)
	}
	_, err = runRoot(t, []string{"tools", "call", "mysql_query", "--params", `{"sql":"SELECT 1"}`, "--config", denyPath})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("denied tool error = %v, want unknown tool", err)
	}
	_, err = runRoot(t, []string{"tools", "call", "mysql_get_server_info", "--config", denyPath})
	if err == nil || err.Error() != "[mysql_configuration_unavailable] configuration unavailable" {
		t.Fatalf("allowed tool error = %v, want configuration unavailable", err)
	}

	clearMySQLMCPStaticConfigEnv(t)
	_, err = runRoot(t, []string{"tools", "call", "mysql_query", "--params", `{"sql":"SELECT 1"}`, "--config", denyPath, "--disabled-tools="})
	if err == nil || err.Error() != "[mysql_configuration_unavailable] configuration unavailable" {
		t.Fatalf("empty flag error = %v, want configuration unavailable", err)
	}

	clearMySQLMCPStaticConfigEnv(t)
	allowPath := writeConfig(t, "enabled_tools: [\" mysql_query \", \" \"]\n")
	out, err = runRoot(t, []string{"tools", "list", "--json", "--config", allowPath})
	if err != nil {
		t.Fatalf("trimmed allowlist: %v", err)
	}
	if names := listNames(out); len(names) != 1 || names[0] != "mysql_query" {
		t.Fatalf("trimmed allowlist = %v, want [mysql_query]", names)
	}

	clearMySQLMCPStaticConfigEnv(t)
	t.Setenv("MYSQL_MCP_ENABLED_TOOLS", "mysql_list_databases")
	out, err = runRoot(t, []string{"tools", "list", "--json", "--config", allowPath})
	if err != nil {
		t.Fatalf("named env allowlist: %v", err)
	}
	if names := listNames(out); len(names) != 1 || names[0] != "mysql_list_databases" {
		t.Fatalf("named env allowlist = %v", names)
	}
	_, err = runRoot(t, []string{"tools", "call", "mysql_list_databases", "--config", allowPath})
	if err == nil || err.Error() != "[mysql_configuration_unavailable] configuration unavailable" {
		t.Fatalf("named env call error = %v, want configuration unavailable", err)
	}
}

type rootOutput struct {
	out string
	err string
}

func runRoot(t *testing.T, args []string) (rootOutput, error) {
	t.Helper()
	return runRootContext(t, time.Minute, args)
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runRootContext(t *testing.T, timeout time.Duration, args []string) (rootOutput, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{In: strings.NewReader(""), Out: &stdout, ErrOut: &stderr})
	root.SetArgs(args)
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	err := root.ExecuteContext(ctx)
	return rootOutput{out: stdout.String(), err: stderr.String()}, err
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func listNames(out rootOutput) []string {
	var rows []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out.out), &rows); err != nil {
		return []string{"PARSE:" + err.Error()}
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}
