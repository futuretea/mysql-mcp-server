package mysql_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	internalmysql "github.com/futuretea/mysql-mcp-server/internal/mysql"
	toolmysql "github.com/futuretea/mysql-mcp-server/pkg/toolset/mysql"
)

func TestToolsetRegistersSixTools(t *testing.T) {
	tools := toolmysql.New(&fakeService{}).GetTools()
	if len(tools) != 6 {
		t.Fatalf("GetTools() count = %d, want 6", len(tools))
	}

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Tool.Name] = true
	}
	for _, name := range []string{
		"mysql_get_server_info",
		"mysql_list_databases",
		"mysql_list_tables",
		"mysql_describe_table",
		"mysql_list_indexes",
		"mysql_query",
	} {
		if !names[name] {
			t.Errorf("GetTools() missing %q", name)
		}
	}
}

func TestToolHandlersRejectInvalidParametersBeforeService(t *testing.T) {
	service := &fakeService{}
	tools := handlersByName(toolmysql.New(service))

	tests := []struct {
		tool   string
		params map[string]any
	}{
		{tool: "mysql_get_server_info", params: map[string]any{"unexpected": "value"}},
		{tool: "mysql_list_databases", params: map[string]any{"unexpected": "value"}},
		{tool: "mysql_list_tables", params: map[string]any{}},
		{tool: "mysql_list_tables", params: map[string]any{"database": ""}},
		{tool: "mysql_describe_table", params: map[string]any{"database": "inventory"}},
		{tool: "mysql_describe_table", params: map[string]any{"database": "inventory", "table": 1}},
		{tool: "mysql_list_indexes", params: map[string]any{"database": "inventory"}},
		{tool: "mysql_list_indexes", params: map[string]any{"database": "inventory", "table": ""}},
		{tool: "mysql_query", params: map[string]any{}},
		{tool: "mysql_query", params: map[string]any{"sql": 1}},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			_, err := tools[tt.tool](context.Background(), tt.params)
			if err == nil || err.Error() != "[mysql_invalid_params] invalid parameters" {
				t.Fatalf("handler error = %v, want mysql_invalid_params", err)
			}
		})
	}
	if service.calls != 0 {
		t.Fatalf("service calls = %d, want 0", service.calls)
	}
}

func TestToolHandlersUseCanonicalJSONEnvelope(t *testing.T) {
	tools := handlersByName(toolmysql.New(&fakeService{}))

	got, err := tools["mysql_get_server_info"](context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	want := `{"schema_version":1,"tool":"mysql_get_server_info","data":{"version":"8.0.46","version_comment":"MySQL","current_database":null}}`
	if got != want {
		t.Fatalf("handler result = %s, want %s", got, want)
	}
}

func TestToolHandlersForwardValidParametersAndUseCanonicalEnvelope(t *testing.T) {
	for _, tt := range validToolHandlerCases {
		t.Run(tt.name, func(t *testing.T) {
			assertToolHandlerResponse(t, tt)
		})
	}
}

type toolHandlerCase struct {
	name, wantCall, wantDatabase, wantTable, wantSQL, wantData string
	params                                                     map[string]any
}

var validToolHandlerCases = []toolHandlerCase{
	{name: "mysql_get_server_info", params: map[string]any{}, wantCall: "get_server_info", wantData: `{"version":"8.0.46","version_comment":"MySQL","current_database":null}`},
	{name: "mysql_list_databases", params: map[string]any{}, wantCall: "list_databases", wantData: `{"databases":["inventory"]}`},
	{name: "mysql_list_tables", params: map[string]any{"database": "inventory"}, wantCall: "list_tables", wantDatabase: "inventory", wantData: `{"database":"inventory","tables":[{"name":"accounts","type":"BASE TABLE"}]}`},
	{name: "mysql_describe_table", params: map[string]any{"database": "inventory", "table": "accounts"}, wantCall: "describe_table", wantDatabase: "inventory", wantTable: "accounts", wantData: `{"database":"inventory","table":"accounts","columns":[{"ordinal_position":1,"name":"id","column_type":"BIGINT","nullable":false,"default":null,"key":"PRI","extra":"","comment":""}]}`},
	{name: "mysql_list_indexes", params: map[string]any{"database": "inventory", "table": "accounts"}, wantCall: "list_indexes", wantDatabase: "inventory", wantTable: "accounts", wantData: `{"database":"inventory","table":"accounts","indexes":[{"name":"PRIMARY","unique":true,"key_parts":[{"sequence_in_index":1,"column_name":"id","expression":null,"sub_part":null,"collation":"A"}]}]}`},
	{name: "mysql_query", params: map[string]any{"sql": "SELECT 1"}, wantCall: "query", wantSQL: "SELECT 1", wantData: `{"columns":[{"name":"one","database":null,"table":null,"type":"BIGINT"}],"rows":[[{"encoding":"integer","value":"1"}]]}`},
}

func assertToolHandlerResponse(t *testing.T, tt toolHandlerCase) {
	t.Helper()
	service := &fakeService{}
	result, err := handlersByName(toolmysql.New(service))[tt.name](context.Background(), tt.params)
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if service.calls != 1 || service.lastCall != tt.wantCall {
		t.Fatalf("service call = %q (%d calls), want %q once", service.lastCall, service.calls, tt.wantCall)
	}
	if service.database != tt.wantDatabase || service.table != tt.wantTable || service.sql != tt.wantSQL {
		t.Fatalf("forwarded args = database:%q table:%q sql:%q, want database:%q table:%q sql:%q", service.database, service.table, service.sql, tt.wantDatabase, tt.wantTable, tt.wantSQL)
	}
	var envelope struct {
		SchemaVersion int             `json:"schema_version"`
		Tool          string          `json:"tool"`
		Data          json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.SchemaVersion != 1 || envelope.Tool != tt.name || string(envelope.Data) != tt.wantData {
		t.Fatalf("envelope = %+v data=%s, want version=1 tool=%q data=%s", envelope, envelope.Data, tt.name, tt.wantData)
	}
}

func TestToolHandlersReturnSafeFixedErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "configuration", err: &internalmysql.Error{Code: internalmysql.CodeConfigurationUnavailable}, want: "[mysql_configuration_unavailable] configuration unavailable"},
		{
			name: "connection",
			err:  &internalmysql.Error{Code: internalmysql.CodeConnectionFailed},
			want: "[mysql_connection_failed] connection failed",
		},
		{name: "query rejected", err: &internalmysql.Error{Code: internalmysql.CodeQueryRejected}, want: "[mysql_query_rejected] query rejected"},
		{
			name: "database code",
			err:  &internalmysql.Error{Code: internalmysql.CodeDatabase},
			want: "[mysql_database_error] database error",
		},
		{
			name: "unknown error",
			err:  errors.New("untrusted driver detail"),
			want: "[mysql_database_error] database error",
		},
		{name: "result encoding", err: &internalmysql.Error{Code: internalmysql.CodeResultEncoding}, want: "[mysql_result_encoding_failed] result encoding failed"},
		{name: "unsafe service role", err: &internalmysql.Error{Code: internalmysql.CodeUnsafeServiceRole}, want: "[mysql_unsafe_service_role] database service role is not permitted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := handlersByName(toolmysql.New(failingService{err: tt.err}))
			_, err := tools["mysql_get_server_info"](context.Background(), map[string]any{})
			if err == nil || err.Error() != tt.want {
				t.Fatalf("handler error = %v, want %q", err, tt.want)
			}
			if err != nil && err.Error() == "untrusted driver detail" {
				t.Fatal("handler exposed source error")
			}
		})
	}
}

func handlersByName(toolset *toolmysql.Toolset) map[string]func(context.Context, map[string]any) (string, error) {
	handlers := make(map[string]func(context.Context, map[string]any) (string, error))
	for _, tool := range toolset.GetTools() {
		handlers[tool.Tool.Name] = tool.Handler
	}
	return handlers
}

type fakeService struct {
	calls    int
	lastCall string
	database string
	table    string
	sql      string
}

type failingService struct {
	err error
}

func (s failingService) GetServerInfo(context.Context) (internalmysql.ServerInfo, error) {
	return internalmysql.ServerInfo{}, s.err
}

func (s failingService) ListDatabases(context.Context) ([]string, error) {
	return nil, s.err
}

func (s failingService) ListTables(context.Context, string) (internalmysql.TablesResult, error) {
	return internalmysql.TablesResult{}, s.err
}

func (s failingService) DescribeTable(context.Context, string, string) (internalmysql.ColumnsResult, error) {
	return internalmysql.ColumnsResult{}, s.err
}

func (s failingService) ListIndexes(context.Context, string, string) (internalmysql.IndexesResult, error) {
	return internalmysql.IndexesResult{}, s.err
}

func (s failingService) Query(context.Context, string) (internalmysql.QueryResult, error) {
	return internalmysql.QueryResult{}, s.err
}

func (s *fakeService) GetServerInfo(context.Context) (internalmysql.ServerInfo, error) {
	s.calls++
	s.lastCall = "get_server_info"
	return internalmysql.ServerInfo{Version: "8.0.46", VersionComment: "MySQL"}, nil
}

func (s *fakeService) ListDatabases(context.Context) ([]string, error) {
	s.calls++
	s.lastCall = "list_databases"
	return []string{"inventory"}, nil
}

func (s *fakeService) ListTables(_ context.Context, database string) (internalmysql.TablesResult, error) {
	s.calls++
	s.lastCall = "list_tables"
	s.database = database
	return internalmysql.TablesResult{Database: database, Tables: []internalmysql.Table{{Name: "accounts", Type: "BASE TABLE"}}}, nil
}

func (s *fakeService) DescribeTable(_ context.Context, database, table string) (internalmysql.ColumnsResult, error) {
	s.calls++
	s.lastCall = "describe_table"
	s.database = database
	s.table = table
	return internalmysql.ColumnsResult{Database: database, Table: table, Columns: []internalmysql.Column{{OrdinalPosition: 1, Name: "id", ColumnType: "BIGINT", Key: "PRI"}}}, nil
}

func (s *fakeService) ListIndexes(_ context.Context, database, table string) (internalmysql.IndexesResult, error) {
	s.calls++
	s.lastCall = "list_indexes"
	s.database = database
	s.table = table
	return internalmysql.IndexesResult{Database: database, Table: table, Indexes: []internalmysql.Index{{Name: "PRIMARY", Unique: true, KeyParts: []internalmysql.KeyPart{{SequenceInIndex: 1, ColumnName: stringPtr("id"), Collation: stringPtr("A")}}}}}, nil
}

func (s *fakeService) Query(_ context.Context, query string) (internalmysql.QueryResult, error) {
	s.calls++
	s.lastCall = "query"
	s.sql = query
	return internalmysql.QueryResult{Columns: []internalmysql.QueryColumn{{Name: "one", Type: "BIGINT"}}, Rows: [][]any{{internalmysql.Cell{Encoding: "integer", Value: "1"}}}}, nil
}

func stringPtr(value string) *string { return &value }
