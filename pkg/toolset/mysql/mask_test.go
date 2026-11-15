package mysql_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	mcpprotocol "github.com/mark3labs/mcp-go/mcp"

	internalmysql "github.com/futuretea/mysql-mcp-server/internal/mysql"
	"github.com/futuretea/mysql-mcp-server/pkg/core/masking"
	mcpserver "github.com/futuretea/mysql-mcp-server/pkg/server/mcp"
	toolmysql "github.com/futuretea/mysql-mcp-server/pkg/toolset/mysql"
)

const (
	sentinel = "synthetic-token-value"
	field    = "synthetic_field"
)

func TestDisabledPolicyKeepsCanonicalToolText(t *testing.T) {
	service := &fakeService{}
	got, err := handlersByName(toolmysql.New(service))["mysql_query"](context.Background(), map[string]any{"sql": "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema_version":1,"tool":"mysql_query","data":{"columns":[{"name":"one","database":null,"table":null,"type":"BIGINT"}],"rows":[[{"encoding":"integer","value":"1"}]]}}`
	if got != want {
		t.Fatalf("legacy query text = %s", got)
	}
	if service.sql != "SELECT 1" {
		t.Fatalf("sql = %q", service.sql)
	}
}

func TestEnabledMaskingCoversEveryToolAndLeavesCommandsUnchanged(t *testing.T) {
	policy := enabledPolicy(t)
	sql := "SELECT '" + sentinel + "'"
	blob := base64.StdEncoding.EncodeToString(append([]byte{0xff}, []byte("[masked]")...))
	service := &recordingService{}
	toolset := toolmysql.New(service)
	toolset.SetPolicy(policy)
	handlers := handlersByName(toolset)

	cases := []struct {
		tool   string
		params map[string]any
		want   string
	}{
		{
			tool:   "mysql_get_server_info",
			params: map[string]any{},
			want:   `{"schema_version":1,"tool":"mysql_get_server_info","data":{"version":"8.0.46","version_comment":"MySQL","current_database":"[masked]"}}`,
		},
		{
			tool:   "mysql_list_databases",
			params: map[string]any{},
			want:   `{"schema_version":1,"tool":"mysql_list_databases","data":{"databases":["inventory","[masked]"]}}`,
		},
		{
			tool:   "mysql_list_tables",
			params: map[string]any{"database": "inventory"},
			want:   `{"schema_version":1,"tool":"mysql_list_tables","data":{"database":"inventory","tables":[{"name":"accounts","type":"BASE TABLE"},{"name":"[masked]","type":"VIEW"}]}}`,
		},
		{
			tool:   "mysql_describe_table",
			params: map[string]any{"database": "inventory", "table": "accounts"},
			want:   `{"schema_version":1,"tool":"mysql_describe_table","data":{"database":"inventory","table":"accounts","columns":[{"ordinal_position":1,"name":"id","column_type":"BIGINT","nullable":false,"default":null,"key":"PRI","extra":"","comment":"[masked]"},{"ordinal_position":2,"name":"[masked]","column_type":"VARCHAR(8)","nullable":true,"default":"[masked]","key":"","extra":"","comment":"ordinary-visible"}]}}`,
		},
		{
			tool:   "mysql_list_indexes",
			params: map[string]any{"database": "inventory", "table": "accounts"},
			want:   `{"schema_version":1,"tool":"mysql_list_indexes","data":{"database":"inventory","table":"accounts","indexes":[{"name":"PRIMARY","unique":true,"key_parts":[{"sequence_in_index":1,"column_name":"id","expression":"[masked]","sub_part":null,"collation":"A"}]}]}}`,
		},
		{
			tool:   "mysql_query",
			params: map[string]any{"sql": sql},
			want:   `{"schema_version":1,"tool":"mysql_query","data":{"columns":[{"name":"title","database":null,"table":null,"type":"VARCHAR"},{"name":"[masked]","database":null,"table":null,"type":"INT"},{"name":"empty","database":null,"table":null,"type":"VARCHAR"},{"name":"wide","database":null,"table":null,"type":"BIGINT UNSIGNED"},{"name":"blob_col","database":null,"table":null,"type":"BLOB"}],"rows":[[{"encoding":"utf8","value":"a[masked]b"},null,null,{"encoding":"integer","value":"18446744073709551615"},{"encoding":"base64","value":"` + blob + `"}]]}}`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.tool, func(t *testing.T) {
			got, err := handlers[tt.tool](context.Background(), tt.params)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("text = %s\nwant %s", got, tt.want)
			}
			if strings.Contains(got, sentinel) || strings.Contains(got, field) {
				t.Fatalf("configured data remains in %s", got)
			}
		})
	}
	if service.sql != sql || service.calls != len(cases) {
		t.Fatalf("service sql = %q calls = %d", service.sql, service.calls)
	}
}

func TestMCPCallUsesMaskedToolTextAndFixedFailure(t *testing.T) {
	policy := enabledPolicy(t)
	service := &recordingService{}
	toolset := toolmysql.New(service)
	toolset.SetPolicy(policy)
	var query toolsetHandler
	for _, tool := range toolset.GetTools() {
		if tool.Tool.Name == "mysql_query" {
			query.handler = tool.Handler
		}
	}
	body, err := query.handler(context.Background(), map[string]any{"sql": "SELECT '" + sentinel + "'"})
	if err != nil {
		t.Fatal(err)
	}
	result := mcpserver.NewTextResult(body, nil)
	text := result.Content[0].(mcpprotocol.TextContent).Text
	if result.IsError || strings.Contains(text, sentinel) || service.sql != "SELECT '"+sentinel+"'" {
		t.Fatalf("mcp text = %s error %v sql %q", text, result.IsError, service.sql)
	}

	overflow := toolmysql.New(&recordingService{overflow: true})
	overflow.SetPolicy(policy)
	for _, tool := range overflow.GetTools() {
		if tool.Tool.Name != "mysql_query" {
			continue
		}
		_, err := tool.Handler(context.Background(), map[string]any{"sql": "SELECT 1"})
		if err == nil || err.Error() != "[mysql_masking_failed] result was not returned" || strings.Contains(err.Error(), sentinel) {
			t.Fatalf("overflow error = %v", err)
		}
	}
}

type toolsetHandler struct {
	handler func(context.Context, map[string]any) (string, error)
}

func enabledPolicy(t *testing.T) masking.Policy {
	t.Helper()
	policy, err := masking.ParseJSON(`{"enabled":true,"names":["synthetic_field"],"values":["synthetic-token-value"],"value_patterns":["x+"]}`)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

type recordingService struct {
	calls    int
	sql      string
	overflow bool
}

func (s *recordingService) GetServerInfo(context.Context) (internalmysql.ServerInfo, error) {
	s.calls++
	database := field
	return internalmysql.ServerInfo{Version: "8.0.46", VersionComment: "MySQL", CurrentDatabase: &database}, nil
}

func (s *recordingService) ListDatabases(context.Context) ([]string, error) {
	s.calls++
	return []string{"inventory", field}, nil
}

func (s *recordingService) ListTables(context.Context, string) (internalmysql.TablesResult, error) {
	s.calls++
	return internalmysql.TablesResult{Database: "inventory", Tables: []internalmysql.Table{
		{Name: "accounts", Type: "BASE TABLE"},
		{Name: field, Type: "VIEW"},
	}}, nil
}

func (s *recordingService) DescribeTable(context.Context, string, string) (internalmysql.ColumnsResult, error) {
	s.calls++
	def := sentinel
	return internalmysql.ColumnsResult{Database: "inventory", Table: "accounts", Columns: []internalmysql.Column{
		{OrdinalPosition: 1, Name: "id", ColumnType: "BIGINT", Nullable: false, Key: "PRI", Comment: sentinel},
		{OrdinalPosition: 2, Name: field, ColumnType: "VARCHAR(8)", Nullable: true, Default: &def, Comment: "ordinary-visible"},
	}}, nil
}

func (s *recordingService) ListIndexes(context.Context, string, string) (internalmysql.IndexesResult, error) {
	s.calls++
	expression := sentinel
	column := "id"
	collation := "A"
	return internalmysql.IndexesResult{Database: "inventory", Table: "accounts", Indexes: []internalmysql.Index{{
		Name: "PRIMARY", Unique: true, KeyParts: []internalmysql.KeyPart{{
			SequenceInIndex: 1, ColumnName: &column, Expression: &expression, Collation: &collation,
		}},
	}}}, nil
}

func (s *recordingService) Query(_ context.Context, query string) (internalmysql.QueryResult, error) {
	s.calls++
	s.sql = query
	if s.overflow {
		return internalmysql.QueryResult{Columns: []internalmysql.QueryColumn{{Name: "blob_col", Type: "BLOB"}}, Rows: [][]any{{internalmysql.Cell{Encoding: "utf8", Value: strings.Repeat("a", 1<<20+1)}}}}, nil
	}
	raw := append([]byte{0xff}, []byte(sentinel)...)
	return internalmysql.QueryResult{
		Columns: []internalmysql.QueryColumn{
			{Name: "title", Type: "VARCHAR"},
			{Name: field, Type: "INT"},
			{Name: "empty", Type: "VARCHAR"},
			{Name: "wide", Type: "BIGINT UNSIGNED"},
			{Name: "blob_col", Type: "BLOB"},
		},
		Rows: [][]any{{
			internalmysql.Cell{Encoding: "utf8", Value: "axxxb"},
			internalmysql.Cell{Encoding: "integer", Value: "1"},
			nil,
			internalmysql.Cell{Encoding: "integer", Value: "18446744073709551615"},
			internalmysql.Cell{Encoding: "base64", Value: base64.StdEncoding.EncodeToString(raw)},
		}},
	}, nil
}
