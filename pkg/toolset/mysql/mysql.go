package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	internalmysql "github.com/futuretea/mysql-mcp-server/internal/mysql"
	"github.com/futuretea/mysql-mcp-server/pkg/toolset"
)

const Domain = "mysql"

var errInvalidParams = errors.New("[mysql_invalid_params] invalid parameters")

var requiredParams = map[string][]string{
	"mysql_get_server_info": {},
	"mysql_list_databases":  {},
	"mysql_list_tables":     {"database"},
	"mysql_describe_table":  {"database", "table"},
	"mysql_list_indexes":    {"database", "table"},
	"mysql_query":           {"sql"},
}

// Toolset exposes the MySQL read-only MCP tools.
type Toolset struct {
	service internalmysql.ReadOnlyService
}

func New(service internalmysql.ReadOnlyService) *Toolset {
	return &Toolset{service: service}
}

func (t *Toolset) GetName() string { return Domain }

func (t *Toolset) GetDescription() string { return "Read-only MySQL discovery and query tools" }

func (t *Toolset) GetTools() []toolset.ServerTool {
	return []toolset.ServerTool{
		{Tool: mcp.NewTool("mysql_get_server_info", mcp.WithDescription("Get MySQL server information")), Handler: t.getServerInfo, Domain: Domain},
		{Tool: mcp.NewTool("mysql_list_databases", mcp.WithDescription("List visible MySQL databases")), Handler: t.listDatabases, Domain: Domain},
		{Tool: mcp.NewTool("mysql_list_tables", mcp.WithDescription("List base tables and views in a database"), mcp.WithString("database", mcp.Required())), Handler: t.listTables, Domain: Domain},
		{Tool: mcp.NewTool("mysql_describe_table", mcp.WithDescription("Describe columns in a MySQL table"), mcp.WithString("database", mcp.Required()), mcp.WithString("table", mcp.Required())), Handler: t.describeTable, Domain: Domain},
		{Tool: mcp.NewTool("mysql_list_indexes", mcp.WithDescription("List indexes in a MySQL table"), mcp.WithString("database", mcp.Required()), mcp.WithString("table", mcp.Required())), Handler: t.listIndexes, Domain: Domain},
		{Tool: mcp.NewTool("mysql_query", mcp.WithDescription("Run one strict read-only MySQL query"), mcp.WithString("sql", mcp.Required())), Handler: t.query, Domain: Domain},
	}
}

// DecodeToolParams enforces the shared tool input contract before any service
// or database call. MCP schemas intentionally mirror, but do not replace it.
func DecodeToolParams(toolName string, params map[string]any) (map[string]string, error) {
	required, known := requiredParams[toolName]
	if !known || len(params) != len(required) {
		return nil, errInvalidParams
	}
	decoded := make(map[string]string, len(required))
	for _, name := range required {
		value, ok := params[name]
		if !ok {
			return nil, errInvalidParams
		}
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, errInvalidParams
		}
		decoded[name] = text
	}
	return decoded, nil
}

func (t *Toolset) getServerInfo(ctx context.Context, params map[string]any) (string, error) {
	if _, err := DecodeToolParams("mysql_get_server_info", params); err != nil {
		return "", err
	}
	data, err := t.service.GetServerInfo(ctx)
	return encodeResponse("mysql_get_server_info", data, err)
}

func (t *Toolset) listDatabases(ctx context.Context, params map[string]any) (string, error) {
	if _, err := DecodeToolParams("mysql_list_databases", params); err != nil {
		return "", err
	}
	databases, err := t.service.ListDatabases(ctx)
	return encodeResponse("mysql_list_databases", map[string]any{"databases": databases}, err)
}

func (t *Toolset) listTables(ctx context.Context, params map[string]any) (string, error) {
	decoded, err := DecodeToolParams("mysql_list_tables", params)
	if err != nil {
		return "", err
	}
	data, err := t.service.ListTables(ctx, decoded["database"])
	return encodeResponse("mysql_list_tables", data, err)
}

func (t *Toolset) describeTable(ctx context.Context, params map[string]any) (string, error) {
	decoded, err := DecodeToolParams("mysql_describe_table", params)
	if err != nil {
		return "", err
	}
	data, err := t.service.DescribeTable(ctx, decoded["database"], decoded["table"])
	return encodeResponse("mysql_describe_table", data, err)
}

func (t *Toolset) listIndexes(ctx context.Context, params map[string]any) (string, error) {
	decoded, err := DecodeToolParams("mysql_list_indexes", params)
	if err != nil {
		return "", err
	}
	data, err := t.service.ListIndexes(ctx, decoded["database"], decoded["table"])
	return encodeResponse("mysql_list_indexes", data, err)
}

func (t *Toolset) query(ctx context.Context, params map[string]any) (string, error) {
	decoded, err := DecodeToolParams("mysql_query", params)
	if err != nil {
		return "", err
	}
	data, err := t.service.Query(ctx, decoded["sql"])
	return encodeResponse("mysql_query", data, err)
}

func encodeResponse(toolName string, data any, err error) (string, error) {
	if err != nil {
		return "", publicError(err)
	}
	payload, err := json.Marshal(struct {
		SchemaVersion int    `json:"schema_version"`
		Tool          string `json:"tool"`
		Data          any    `json:"data"`
	}{SchemaVersion: 1, Tool: toolName, Data: data})
	if err != nil {
		return "", publicError(codeError(internalmysql.CodeResultEncoding))
	}
	return string(payload), nil
}

func codeError(code internalmysql.ErrorCode) error {
	return &internalmysql.Error{Code: code}
}

func publicError(err error) error {
	return internalmysql.PublicError(err)
}

var _ toolset.Toolset = (*Toolset)(nil)
