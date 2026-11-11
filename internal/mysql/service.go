package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/rs/zerolog/log"
)

type ErrorCode string

const (
	CodeConfigurationUnavailable ErrorCode = "configuration_unavailable"
	CodeConnectionFailed         ErrorCode = "connection_failed"
	CodeQueryRejected            ErrorCode = "query_rejected"
	CodeDatabase                 ErrorCode = "database_error"
	CodeResultEncoding           ErrorCode = "result_encoding_failed"
	CodeUnsafeServiceRole        ErrorCode = "unsafe_service_role"
)

// publicErrorMessages are the only MySQL failure strings returned to CLI and MCP callers.
var publicErrorMessages = map[ErrorCode]string{
	CodeConfigurationUnavailable: "[mysql_configuration_unavailable] configuration unavailable",
	CodeConnectionFailed:         "[mysql_connection_failed] connection failed",
	CodeQueryRejected:            "[mysql_query_rejected] query rejected",
	CodeDatabase:                 "[mysql_database_error] database error",
	CodeResultEncoding:           "[mysql_result_encoding_failed] result encoding failed",
	CodeUnsafeServiceRole:        "[mysql_unsafe_service_role] database service role is not permitted",
}

// PublicError maps an internal failure to a fixed caller-facing message.
// Unknown errors use the database failure message and do not preserve driver text.
func PublicError(err error) error {
	code, ok := CodeOf(err)
	if !ok {
		code = CodeDatabase
	}
	message, ok := publicErrorMessages[code]
	if !ok {
		message = publicErrorMessages[CodeDatabase]
	}
	return errors.New(message)
}

// Error carries an internal failure category without preserving database input
// or driver error text for public transports.
type Error struct {
	Code ErrorCode
}

func (e *Error) Error() string { return string(e.Code) }

func codeError(code ErrorCode) error { return &Error{Code: code} }

func CodeOf(err error) (ErrorCode, bool) {
	var mysqlError *Error
	if errors.As(err, &mysqlError) {
		return mysqlError.Code, true
	}
	return "", false
}

type ServerInfo struct {
	Version         string  `json:"version"`
	VersionComment  string  `json:"version_comment"`
	CurrentDatabase *string `json:"current_database"`
}

type Table struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type TablesResult struct {
	Database string  `json:"database"`
	Tables   []Table `json:"tables"`
}

type Column struct {
	OrdinalPosition int     `json:"ordinal_position"`
	Name            string  `json:"name"`
	ColumnType      string  `json:"column_type"`
	Nullable        bool    `json:"nullable"`
	Default         *string `json:"default"`
	Key             string  `json:"key"`
	Extra           string  `json:"extra"`
	Comment         string  `json:"comment"`
}

type ColumnsResult struct {
	Database string   `json:"database"`
	Table    string   `json:"table"`
	Columns  []Column `json:"columns"`
}

type KeyPart struct {
	SequenceInIndex int     `json:"sequence_in_index"`
	ColumnName      *string `json:"column_name"`
	Expression      *string `json:"expression"`
	SubPart         *int    `json:"sub_part"`
	Collation       *string `json:"collation"`
}

type Index struct {
	Name     string    `json:"name"`
	Unique   bool      `json:"unique"`
	KeyParts []KeyPart `json:"key_parts"`
}

type IndexesResult struct {
	Database string  `json:"database"`
	Table    string  `json:"table"`
	Indexes  []Index `json:"indexes"`
}

type QueryColumn struct {
	Name     string  `json:"name"`
	Database *string `json:"database"`
	Table    *string `json:"table"`
	Type     string  `json:"type"`
}

type QueryResult struct {
	Columns []QueryColumn `json:"columns"`
	Rows    [][]any       `json:"rows"`
}

// ReadOnlyService is the private MySQL toolset dependency.
type ReadOnlyService interface {
	GetServerInfo(context.Context) (ServerInfo, error)
	ListDatabases(context.Context) ([]string, error)
	ListTables(context.Context, string) (TablesResult, error)
	DescribeTable(context.Context, string, string) (ColumnsResult, error)
	ListIndexes(context.Context, string, string) (IndexesResult, error)
	Query(context.Context, string) (QueryResult, error)
}

// Service performs all MySQL operations through one database handle.
type Service struct {
	db *sql.DB
}

// NewService opens one MySQL handle, connects, and verifies the service account
// before any tool can run SQL. allowUnsafeDatabaseRole skips only the
// unsafe-privilege rejection; connection and grant-probe failures still abort.
func NewService(ctx context.Context, dsn string, allowUnsafeDatabaseRole bool) (*Service, error) {
	return newService(ctx, dsn, allowUnsafeDatabaseRole, func(dsn string) (*sql.DB, error) {
		return sql.Open("mysql", dsn)
	})
}

func newService(ctx context.Context, dsn string, allowUnsafeDatabaseRole bool, open func(string) (*sql.DB, error)) (*Service, error) {
	if dsn == "" {
		return nil, codeError(CodeConfigurationUnavailable)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if open == nil {
		return nil, codeError(CodeConfigurationUnavailable)
	}
	db, err := open(dsn)
	if err != nil || db == nil {
		return nil, codeError(CodeConfigurationUnavailable)
	}
	service := &Service{db: db}
	if err := service.verifyStartup(ctx, allowUnsafeDatabaseRole); err != nil {
		_ = service.Close()
		return nil, err
	}
	return service, nil
}

func NewUnavailableService() ReadOnlyService { return unavailableService{} }

func (s *Service) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Service) GetServerInfo(ctx context.Context) (ServerInfo, error) {
	var result ServerInfo
	var current sql.NullString
	if err := s.db.QueryRowContext(ctx, "SELECT VERSION(), @@version_comment, DATABASE()").Scan(&result.Version, &result.VersionComment, &current); err != nil {
		return ServerInfo{}, databaseError("get_server_info.query", err)
	}
	if current.Valid {
		result.CurrentDatabase = &current.String
	}
	return result, nil
}

func (s *Service) ListDatabases(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT SCHEMA_NAME FROM INFORMATION_SCHEMA.SCHEMATA ORDER BY SCHEMA_NAME")
	if err != nil {
		return nil, databaseError("list_databases.query", err)
	}
	defer func() { _ = rows.Close() }()

	databases := make([]string, 0)
	for rows.Next() {
		var database string
		if err := rows.Scan(&database); err != nil {
			return nil, databaseError("list_databases.scan", err)
		}
		databases = append(databases, database)
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("list_databases.rows", err)
	}
	return databases, nil
}

func (s *Service) ListTables(ctx context.Context, database string) (TablesResult, error) {
	result := TablesResult{Database: database, Tables: make([]Table, 0)}
	rows, err := s.db.QueryContext(ctx, "SELECT TABLE_NAME, TABLE_TYPE FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE IN ('BASE TABLE', 'VIEW') ORDER BY TABLE_NAME", database)
	if err != nil {
		return TablesResult{}, databaseError("list_tables.query", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var table Table
		if err := rows.Scan(&table.Name, &table.Type); err != nil {
			return TablesResult{}, databaseError("list_tables.scan", err)
		}
		result.Tables = append(result.Tables, table)
	}
	if err := rows.Err(); err != nil {
		return TablesResult{}, databaseError("list_tables.rows", err)
	}
	return result, nil
}

func (s *Service) DescribeTable(ctx context.Context, database, tableName string) (ColumnsResult, error) {
	result := ColumnsResult{Database: database, Table: tableName, Columns: make([]Column, 0)}
	rows, err := s.db.QueryContext(ctx, "SELECT ORDINAL_POSITION, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_KEY, EXTRA, COLUMN_COMMENT FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION", database, tableName)
	if err != nil {
		return ColumnsResult{}, databaseError("describe_table.query", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var column Column
		var nullable string
		var defaultValue sql.NullString
		if err := rows.Scan(&column.OrdinalPosition, &column.Name, &column.ColumnType, &nullable, &defaultValue, &column.Key, &column.Extra, &column.Comment); err != nil {
			return ColumnsResult{}, databaseError("describe_table.scan", err)
		}
		column.Nullable = nullable == "YES"
		if defaultValue.Valid {
			column.Default = &defaultValue.String
		}
		result.Columns = append(result.Columns, column)
	}
	if err := rows.Err(); err != nil {
		return ColumnsResult{}, databaseError("describe_table.rows", err)
	}
	return result, nil
}

func (s *Service) ListIndexes(ctx context.Context, database, tableName string) (IndexesResult, error) {
	result := IndexesResult{Database: database, Table: tableName, Indexes: make([]Index, 0)}
	rows, err := s.db.QueryContext(ctx, "SELECT INDEX_NAME, NON_UNIQUE, SEQ_IN_INDEX, COLUMN_NAME, EXPRESSION, SUB_PART, COLLATION FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY INDEX_NAME, SEQ_IN_INDEX", database, tableName)
	if err != nil {
		return IndexesResult{}, databaseError("list_indexes.query", err)
	}
	defer func() { _ = rows.Close() }()
	indexes := map[string]int{}
	for rows.Next() {
		var name string
		var nonUnique int
		var part KeyPart
		var columnName, expression, collation sql.NullString
		var subPart sql.NullInt64
		if err := rows.Scan(&name, &nonUnique, &part.SequenceInIndex, &columnName, &expression, &subPart, &collation); err != nil {
			return IndexesResult{}, databaseError("list_indexes.scan", err)
		}
		if columnName.Valid {
			part.ColumnName = &columnName.String
		}
		if expression.Valid {
			part.Expression = &expression.String
		}
		if subPart.Valid {
			value := int(subPart.Int64)
			part.SubPart = &value
		}
		if collation.Valid {
			part.Collation = &collation.String
		}
		position, ok := indexes[name]
		if !ok {
			position = len(result.Indexes)
			indexes[name] = position
			result.Indexes = append(result.Indexes, Index{Name: name, Unique: nonUnique == 0})
		}
		result.Indexes[position].KeyParts = append(result.Indexes[position].KeyParts, part)
	}
	if err := rows.Err(); err != nil {
		return IndexesResult{}, databaseError("list_indexes.rows", err)
	}
	return result, nil
}

func (s *Service) Query(ctx context.Context, query string) (QueryResult, error) {
	if err := ValidateQuery(query); err != nil {
		return QueryResult{}, codeError(CodeQueryRejected)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return QueryResult{}, databaseError("query.begin", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return QueryResult{}, databaseError("query.execute", err)
	}
	defer func() { _ = rows.Close() }()

	columnNames, err := rows.Columns()
	if err != nil {
		return QueryResult{}, databaseError("query.columns", err)
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return QueryResult{}, databaseError("query.column_types", err)
	}
	result := QueryResult{Columns: make([]QueryColumn, len(columnNames)), Rows: make([][]any, 0)}
	for index, name := range columnNames {
		result.Columns[index] = QueryColumn{Name: name, Type: columnTypes[index].DatabaseTypeName()}
	}
	for rows.Next() {
		values := make([]any, len(columnNames))
		targets := make([]any, len(columnNames))
		for index := range values {
			targets[index] = &values[index]
		}
		if err := rows.Scan(targets...); err != nil {
			return QueryResult{}, databaseError("query.scan", err)
		}
		encoded := make([]any, len(values))
		for index, value := range values {
			cell, err := EncodeValue(value)
			if err != nil {
				return QueryResult{}, codeError(CodeResultEncoding)
			}
			encoded[index] = cell
		}
		result.Rows = append(result.Rows, encoded)
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, databaseError("query.rows", err)
	}
	return result, nil
}

func databaseError(operation string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
		return codeError(CodeConnectionFailed)
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return codeError(CodeConnectionFailed)
	}
	logDatabaseError(operation, err)
	return codeError(CodeDatabase)
}

func logDatabaseError(operation string, err error) {
	event := log.Error().Str("operation", operation)
	var mysqlError *mysqldriver.MySQLError
	if errors.As(err, &mysqlError) {
		event = event.Uint16("mysql_error_number", mysqlError.Number).Str("mysql_sql_state", string(mysqlError.SQLState[:]))
	} else {
		event = event.Str("error_type", fmt.Sprintf("%T", err))
	}
	event.Msg("MySQL operation failed")
}

type unavailableService struct{}

func (unavailableService) GetServerInfo(context.Context) (ServerInfo, error) {
	return ServerInfo{}, codeError(CodeConfigurationUnavailable)
}

func (unavailableService) ListDatabases(context.Context) ([]string, error) {
	return nil, codeError(CodeConfigurationUnavailable)
}

func (unavailableService) ListTables(context.Context, string) (TablesResult, error) {
	return TablesResult{}, codeError(CodeConfigurationUnavailable)
}

func (unavailableService) DescribeTable(context.Context, string, string) (ColumnsResult, error) {
	return ColumnsResult{}, codeError(CodeConfigurationUnavailable)
}

func (unavailableService) ListIndexes(context.Context, string, string) (IndexesResult, error) {
	return IndexesResult{}, codeError(CodeConfigurationUnavailable)
}

func (unavailableService) Query(context.Context, string) (QueryResult, error) {
	return QueryResult{}, codeError(CodeConfigurationUnavailable)
}

var _ ReadOnlyService = (*Service)(nil)
