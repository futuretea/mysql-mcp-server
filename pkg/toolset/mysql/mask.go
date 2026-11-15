package mysql

import (
	"encoding/base64"
	"unicode/utf8"

	internalmysql "github.com/futuretea/mysql-mcp-server/internal/mysql"
	"github.com/futuretea/mysql-mcp-server/pkg/core/masking"
)

const resultMaskedMessage = "[mysql_masking_failed] result was not returned"

func (t *Toolset) maskData(data any) (any, error) {
	if !t.policy.Enabled() {
		return data, nil
	}
	budget := &masking.Budget{}
	switch typed := data.(type) {
	case internalmysql.ServerInfo:
		return t.maskServer(typed, budget)
	case map[string]any:
		return t.maskDatabaseList(typed, budget)
	case internalmysql.TablesResult:
		return t.maskTables(typed, budget)
	case internalmysql.ColumnsResult:
		return t.maskColumns(typed, budget)
	case internalmysql.IndexesResult:
		return t.maskIndexes(typed, budget)
	case internalmysql.QueryResult:
		return t.maskQuery(typed, budget)
	default:
		return nil, masking.ErrFailed
	}
}

func (t *Toolset) maskServer(info internalmysql.ServerInfo, budget *masking.Budget) (internalmysql.ServerInfo, error) {
	version, err := t.policy.MaskText(info.Version, budget)
	if err != nil {
		return internalmysql.ServerInfo{}, err
	}
	comment, err := t.policy.MaskText(info.VersionComment, budget)
	if err != nil {
		return internalmysql.ServerInfo{}, err
	}
	database, err := maskOptionalName(t.policy, info.CurrentDatabase, budget)
	if err != nil {
		return internalmysql.ServerInfo{}, err
	}
	info.Version = version
	info.VersionComment = comment
	info.CurrentDatabase = database
	return info, nil
}

func (t *Toolset) maskDatabaseList(data map[string]any, budget *masking.Budget) (map[string]any, error) {
	raw, ok := data["databases"]
	if !ok || len(data) != 1 {
		return nil, masking.ErrFailed
	}
	names, ok := raw.([]string)
	if !ok {
		return nil, masking.ErrFailed
	}
	masked := make([]string, len(names))
	for i, name := range names {
		value, err := maskIdentifier(t.policy, name, budget)
		if err != nil {
			return nil, err
		}
		masked[i] = value
	}
	return map[string]any{"databases": masked}, nil
}

func (t *Toolset) maskTables(result internalmysql.TablesResult, budget *masking.Budget) (internalmysql.TablesResult, error) {
	database, err := maskIdentifier(t.policy, result.Database, budget)
	if err != nil {
		return internalmysql.TablesResult{}, err
	}
	tables := make([]internalmysql.Table, len(result.Tables))
	for i, table := range result.Tables {
		name, err := maskIdentifier(t.policy, table.Name, budget)
		if err != nil {
			return internalmysql.TablesResult{}, err
		}
		kind, err := t.policy.MaskText(table.Type, budget)
		if err != nil {
			return internalmysql.TablesResult{}, err
		}
		tables[i] = internalmysql.Table{Name: name, Type: kind}
	}
	return internalmysql.TablesResult{Database: database, Tables: tables}, nil
}

func (t *Toolset) maskColumns(result internalmysql.ColumnsResult, budget *masking.Budget) (internalmysql.ColumnsResult, error) {
	database, err := maskIdentifier(t.policy, result.Database, budget)
	if err != nil {
		return internalmysql.ColumnsResult{}, err
	}
	table, err := maskIdentifier(t.policy, result.Table, budget)
	if err != nil {
		return internalmysql.ColumnsResult{}, err
	}
	columns := make([]internalmysql.Column, len(result.Columns))
	for i, column := range result.Columns {
		name, err := maskIdentifier(t.policy, column.Name, budget)
		if err != nil {
			return internalmysql.ColumnsResult{}, err
		}
		columnType, err := t.policy.MaskText(column.ColumnType, budget)
		if err != nil {
			return internalmysql.ColumnsResult{}, err
		}
		key, err := t.policy.MaskText(column.Key, budget)
		if err != nil {
			return internalmysql.ColumnsResult{}, err
		}
		extra, err := t.policy.MaskText(column.Extra, budget)
		if err != nil {
			return internalmysql.ColumnsResult{}, err
		}
		comment, err := t.policy.MaskText(column.Comment, budget)
		if err != nil {
			return internalmysql.ColumnsResult{}, err
		}
		def, err := maskOptionalText(t.policy, column.Default, budget)
		if err != nil {
			return internalmysql.ColumnsResult{}, err
		}
		columns[i] = internalmysql.Column{
			OrdinalPosition: column.OrdinalPosition,
			Name:            name,
			ColumnType:      columnType,
			Nullable:        column.Nullable,
			Default:         def,
			Key:             key,
			Extra:           extra,
			Comment:         comment,
		}
	}
	return internalmysql.ColumnsResult{Database: database, Table: table, Columns: columns}, nil
}

func (t *Toolset) maskIndexes(result internalmysql.IndexesResult, budget *masking.Budget) (internalmysql.IndexesResult, error) {
	database, err := maskIdentifier(t.policy, result.Database, budget)
	if err != nil {
		return internalmysql.IndexesResult{}, err
	}
	table, err := maskIdentifier(t.policy, result.Table, budget)
	if err != nil {
		return internalmysql.IndexesResult{}, err
	}
	indexes := make([]internalmysql.Index, len(result.Indexes))
	for i, index := range result.Indexes {
		name, err := maskIdentifier(t.policy, index.Name, budget)
		if err != nil {
			return internalmysql.IndexesResult{}, err
		}
		parts := make([]internalmysql.KeyPart, len(index.KeyParts))
		for j, part := range index.KeyParts {
			column, err := maskOptionalName(t.policy, part.ColumnName, budget)
			if err != nil {
				return internalmysql.IndexesResult{}, err
			}
			expression, err := maskOptionalText(t.policy, part.Expression, budget)
			if err != nil {
				return internalmysql.IndexesResult{}, err
			}
			collation, err := maskOptionalText(t.policy, part.Collation, budget)
			if err != nil {
				return internalmysql.IndexesResult{}, err
			}
			parts[j] = internalmysql.KeyPart{
				SequenceInIndex: part.SequenceInIndex,
				ColumnName:      column,
				Expression:      expression,
				SubPart:         part.SubPart,
				Collation:       collation,
			}
		}
		indexes[i] = internalmysql.Index{Name: name, Unique: index.Unique, KeyParts: parts}
	}
	return internalmysql.IndexesResult{Database: database, Table: table, Indexes: indexes}, nil
}

func (t *Toolset) maskQuery(result internalmysql.QueryResult, budget *masking.Budget) (internalmysql.QueryResult, error) {
	columns := make([]internalmysql.QueryColumn, len(result.Columns))
	seen := make(map[string]struct{}, len(result.Columns))
	for i, column := range result.Columns {
		name, err := maskIdentifier(t.policy, column.Name, budget)
		if err != nil {
			return internalmysql.QueryResult{}, err
		}
		if _, ok := seen[name]; ok {
			return internalmysql.QueryResult{}, masking.ErrFailed
		}
		seen[name] = struct{}{}
		kind, err := t.policy.MaskText(column.Type, budget)
		if err != nil {
			return internalmysql.QueryResult{}, err
		}
		database, err := maskOptionalName(t.policy, column.Database, budget)
		if err != nil {
			return internalmysql.QueryResult{}, err
		}
		table, err := maskOptionalName(t.policy, column.Table, budget)
		if err != nil {
			return internalmysql.QueryResult{}, err
		}
		columns[i] = internalmysql.QueryColumn{Name: name, Database: database, Table: table, Type: kind}
	}
	rows := make([][]any, len(result.Rows))
	for i, row := range result.Rows {
		if len(row) != len(columns) {
			return internalmysql.QueryResult{}, masking.ErrFailed
		}
		masked := make([]any, len(row))
		for j, cell := range row {
			value, err := maskCell(t.policy, cell, t.policy.MatchName(result.Columns[j].Name), budget)
			if err != nil {
				return internalmysql.QueryResult{}, err
			}
			masked[j] = value
		}
		rows[i] = masked
	}
	return internalmysql.QueryResult{Columns: columns, Rows: rows}, nil
}

func maskCell(policy masking.Policy, cell any, nameMatched bool, budget *masking.Budget) (any, error) {
	if cell == nil {
		return nil, nil
	}
	typed, ok := cell.(internalmysql.Cell)
	if !ok {
		return nil, masking.ErrFailed
	}
	if nameMatched {
		switch typed.Encoding {
		case "utf8", "base64":
			return internalmysql.Cell{Encoding: "utf8", Value: policy.Placeholder()}, nil
		case "integer", "float":
			return nil, nil
		default:
			return nil, masking.ErrFailed
		}
	}
	switch typed.Encoding {
	case "utf8":
		text, err := policy.MaskText(typed.Value, budget)
		if err != nil {
			return nil, err
		}
		return internalmysql.Cell{Encoding: "utf8", Value: text}, nil
	case "base64":
		if !masking.WithinInput(len(typed.Value)) {
			return nil, masking.ErrFailed
		}
		raw, err := base64.StdEncoding.DecodeString(typed.Value)
		if err != nil {
			return nil, masking.ErrFailed
		}
		masked, err := policy.MaskBytes(raw, budget)
		if err != nil {
			return nil, err
		}
		if utf8.Valid(masked) {
			return internalmysql.Cell{Encoding: "utf8", Value: string(masked)}, nil
		}
		return internalmysql.Cell{Encoding: "base64", Value: base64.StdEncoding.EncodeToString(masked)}, nil
	case "integer", "float":
		hit, err := policy.MatchScalar(typed.Value, budget)
		if err != nil {
			return nil, err
		}
		if hit {
			return nil, nil
		}
		return typed, nil
	default:
		return nil, masking.ErrFailed
	}
}

func maskIdentifier(policy masking.Policy, value string, budget *masking.Budget) (string, error) {
	if policy.MatchName(value) {
		return policy.Placeholder(), nil
	}
	return policy.MaskText(value, budget)
}

func maskOptionalName(policy masking.Policy, value *string, budget *masking.Budget) (*string, error) {
	if value == nil {
		return nil, nil
	}
	masked, err := maskIdentifier(policy, *value, budget)
	if err != nil {
		return nil, err
	}
	return &masked, nil
}

func maskOptionalText(policy masking.Policy, value *string, budget *masking.Budget) (*string, error) {
	if value == nil {
		return nil, nil
	}
	masked, err := policy.MaskText(*value, budget)
	if err != nil {
		return nil, err
	}
	return &masked, nil
}
