package mysql

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

const canonicalSampleQueryResponse = `{"schema_version":1,"tool":"mysql_query","data":{"columns":[{"name":"id","database":null,"table":null,"type":"INT"},{"name":"name","database":null,"table":null,"type":"VARCHAR"}],"rows":[[{"encoding":"integer","value":"1"},{"encoding":"utf8","value":"hello"}]]}}`

func assertExternalEntryPoints(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	binary := buildMCPServerBinary(t, ctx)

	assertCompiledCLIQuery(t, ctx, binary, dsn)
	assertCompiledCLIRejectsQuery(t, ctx, binary, dsn)
	assertCompiledCLIRejectsUnreachableServer(t, ctx, binary)
	assertCompiledMCPStdio(t, ctx, binary, dsn)
}

func buildMCPServerBinary(t *testing.T, ctx context.Context) string {
	t.Helper()
	root := repositoryRoot(t, ctx)
	binary := filepath.Join(t.TempDir(), "mysql-mcp-server")
	command := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/mcp-server")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build MCP server binary: %v\n%s", err, output)
	}
	return binary
}

func repositoryRoot(t *testing.T, ctx context.Context) string {
	t.Helper()
	output, err := exec.CommandContext(ctx, "go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("locate Go module: %v", err)
	}
	goMod := strings.TrimSpace(string(output))
	if goMod == "" || goMod == os.DevNull {
		t.Fatal("locate Go module: GOMOD is empty")
	}
	return filepath.Dir(goMod)
}

func assertCompiledCLIQuery(t *testing.T, ctx context.Context, binary, dsn string) {
	t.Helper()
	stdout, stderr, err := runMCPServer(t, ctx, binary, dsn, true,
		"tools", "call", "mysql_query", "--params", `{"sql":"SELECT id, name FROM sample ORDER BY id"}`)
	if err != nil {
		t.Fatalf("compiled CLI valid query: %v\nstderr: %s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("compiled CLI valid query wrote stderr: %q", stderr)
	}
	if stdout != canonicalSampleQueryResponse+"\n" {
		t.Fatalf("compiled CLI response = %q, want %q", stdout, canonicalSampleQueryResponse+"\n")
	}
}

func assertCompiledCLIRejectsQuery(t *testing.T, ctx context.Context, binary, dsn string) {
	t.Helper()
	stdout, stderr, err := runMCPServer(t, ctx, binary, dsn, true,
		"tools", "call", "mysql_query", "--params", `{"sql":"SELECT UUID_SHORT()"}`)
	if err == nil {
		t.Fatal("compiled CLI accepted a rejected query")
	}
	if stdout != "" {
		t.Fatalf("compiled CLI rejected query wrote stdout: %q", stdout)
	}
	if stderr != "[mysql_query_rejected] query rejected\n" {
		t.Fatalf("compiled CLI rejection = %q, want query_rejected", stderr)
	}
}

func assertCompiledCLIRejectsUnreachableServer(t *testing.T, ctx context.Context, binary string) {
	t.Helper()
	stdout, stderr, err := runMCPServer(t, ctx, binary, unavailableMySQLDSN(t), false,
		"tools", "call", "mysql_query", "--params", `{"sql":"SELECT UUID_SHORT()"}`)
	if err == nil {
		t.Fatal("compiled CLI started against an unreachable MySQL server")
	}
	if stdout != "" {
		t.Fatalf("compiled CLI unreachable startup wrote stdout: %q", stdout)
	}
	if stderr != "[mysql_connection_failed] connection failed\n" {
		t.Fatalf("compiled CLI unreachable startup = %q, want connection_failed", stderr)
	}
}

func unavailableMySQLDSN(t *testing.T) string {
	t.Helper()
	runtimeConfig := mysqldriver.NewConfig()
	runtimeConfig.User = "root"
	runtimeConfig.Net = "tcp"
	runtimeConfig.Addr = "127.0.0.1:1"
	runtimeConfig.DBName = "mysql_mcp_test"
	return runtimeConfig.FormatDSN()
}

func assertCompiledMCPStdio(t *testing.T, ctx context.Context, binary, dsn string) {
	t.Helper()
	client, err := mcpclient.NewStdioMCPClient(binary, rootFixtureEnvironment(t, dsn), "mcp")
	if err != nil {
		t.Fatalf("start MCP stdio client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start MCP client: %v", err)
	}
	if _, err := client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{ClientInfo: mcp.Implementation{Name: "mysql-mcp-e2e", Version: "test"}},
	}); err != nil {
		t.Fatalf("initialize MCP client: %v", err)
	}

	tools, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list MCP tools: %v", err)
	}
	assertMySQLToolsDiscovered(t, tools.Tools)

	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "mysql_query",
			Arguments: map[string]any{"sql": "SELECT id, name FROM sample ORDER BY id"},
		},
	})
	if err != nil {
		t.Fatalf("call MCP mysql_query: %v", err)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("MCP mysql_query result = %+v, want one success text content", result)
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("MCP mysql_query content = %T, want mcp.TextContent", result.Content[0])
	}
	if text.Text != canonicalSampleQueryResponse {
		t.Fatalf("MCP mysql_query response = %q, want %q", text.Text, canonicalSampleQueryResponse)
	}
}

func runMCPServer(t *testing.T, ctx context.Context, binary, dsn string, allowUnsafeRole bool, args ...string) (string, string, error) {
	t.Helper()
	command := exec.CommandContext(ctx, binary, args...)
	if allowUnsafeRole {
		command.Env = rootFixtureEnvironment(t, dsn)
	} else {
		command.Env = runtimeEnvironment(t, dsn)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

func runtimeEnvironment(t *testing.T, dsn string) []string {
	t.Helper()
	runtimeConfig, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse fixture DSN: %v", err)
	}
	if runtimeConfig.Net != "tcp" {
		t.Fatalf("fixture network = %q, want tcp", runtimeConfig.Net)
	}
	host, port, err := net.SplitHostPort(runtimeConfig.Addr)
	if err != nil {
		t.Fatalf("split fixture address %q: %v", runtimeConfig.Addr, err)
	}

	environment := make([]string, 0, len(os.Environ())+5)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "MYSQL_DSN" || strings.HasPrefix(key, "MYSQL_MCP_") || strings.HasPrefix(key, "MCP_") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"MYSQL_MCP_HOST="+host,
		"MYSQL_MCP_DB_PORT="+port,
		"MYSQL_MCP_USERNAME="+runtimeConfig.User,
		"MYSQL_MCP_PASSWORD="+runtimeConfig.Passwd,
		"MYSQL_MCP_DATABASE="+runtimeConfig.DBName,
	)
}

func rootFixtureEnvironment(t *testing.T, dsn string) []string {
	t.Helper()
	// The disposable server is started as root. Opt out of the privilege
	// rejection so the CLI and MCP smoke tests can exercise query behavior.
	return append(runtimeEnvironment(t, dsn), "MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE=true")
}

func TestRuntimeEnvironmentSeparatesTCPDSNIntoMySQLMCPFields(t *testing.T) {
	t.Setenv("MYSQL_DSN", "legacy@tcp(127.0.0.1:3306)/legacy")
	t.Setenv("MYSQL_MCP_HOST", "inherited-host")

	rawConfig := mysqldriver.NewConfig()
	rawConfig.User = "reader"
	rawConfig.Passwd = "fixture-pass"
	rawConfig.Net = "tcp"
	rawConfig.Addr = "127.0.0.1:43306"
	rawConfig.DBName = "inventory"
	environment := environmentValues(runtimeEnvironment(t, rawConfig.FormatDSN()))

	for key, want := range map[string]string{
		"MYSQL_MCP_HOST":     "127.0.0.1",
		"MYSQL_MCP_DB_PORT":  "43306",
		"MYSQL_MCP_USERNAME": "reader",
		"MYSQL_MCP_PASSWORD": "fixture-pass",
		"MYSQL_MCP_DATABASE": "inventory",
	} {
		if got := environment[key]; got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if _, exists := environment["MYSQL_DSN"]; exists {
		t.Fatalf("runtime environment retained MYSQL_DSN: %v", environment)
	}
}

func environmentValues(entries []string) map[string]string {
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	return values
}

func assertMySQLToolsDiscovered(t *testing.T, tools []mcp.Tool) {
	t.Helper()
	got := make(map[string]bool, len(tools))
	for _, tool := range tools {
		got[tool.Name] = true
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
			t.Fatalf("MCP tools/list did not include %q: %v", name, toolNames(tools))
		}
	}
}

func toolNames(tools []mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}
