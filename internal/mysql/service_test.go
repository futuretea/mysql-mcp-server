package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
)

func TestServiceQueryUsesReadOnlyTransactionAndRollsBack(t *testing.T) {
	conn := &transactionGuardConn{}
	service := &Service{db: sql.OpenDB(transactionGuardConnector{conn: conn})}
	defer func() { _ = service.Close() }()

	result, err := service.Query(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !conn.beginCalled || !conn.readOnly {
		t.Fatalf("BeginTx called=%t readOnly=%t, want true/true", conn.beginCalled, conn.readOnly)
	}
	if !conn.queryCalled {
		t.Fatal("Query did not execute through the transaction connection")
	}
	if !conn.rolledBack {
		t.Fatal("Query did not roll back the transaction")
	}
	if len(result.Rows) != 1 {
		t.Fatalf("Query rows = %d, want 1", len(result.Rows))
	}
}

func TestServiceDiscoveryUsesFixedQueriesAndBoundParameters(t *testing.T) {
	conn := &metadataCaptureConn{}
	service := &Service{db: sql.OpenDB(metadataCaptureConnector{conn: conn})}
	defer func() { _ = service.Close() }()

	tests := []struct {
		name      string
		call      func() error
		wantQuery string
		wantArgs  []driver.Value
	}{
		{
			name:      "list databases",
			call:      func() error { _, err := service.ListDatabases(context.Background()); return err },
			wantQuery: "SELECT SCHEMA_NAME FROM INFORMATION_SCHEMA.SCHEMATA ORDER BY SCHEMA_NAME",
		},
		{
			name:      "list tables",
			call:      func() error { _, err := service.ListTables(context.Background(), "inventory's archive"); return err },
			wantQuery: "SELECT TABLE_NAME, TABLE_TYPE FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE IN ('BASE TABLE', 'VIEW') ORDER BY TABLE_NAME",
			wantArgs:  []driver.Value{"inventory's archive"},
		},
		{
			name: "describe table",
			call: func() error {
				_, err := service.DescribeTable(context.Background(), "inventory's archive", "account history")
				return err
			},
			wantQuery: "SELECT ORDINAL_POSITION, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_KEY, EXTRA, COLUMN_COMMENT FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION",
			wantArgs:  []driver.Value{"inventory's archive", "account history"},
		},
		{
			name: "list indexes",
			call: func() error {
				_, err := service.ListIndexes(context.Background(), "inventory's archive", "account history")
				return err
			},
			wantQuery: "SELECT INDEX_NAME, NON_UNIQUE, SEQ_IN_INDEX, COLUMN_NAME, EXPRESSION, SUB_PART, COLLATION FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY INDEX_NAME, SEQ_IN_INDEX",
			wantArgs:  []driver.Value{"inventory's archive", "account history"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn.reset()
			if err := tt.call(); err != nil {
				t.Fatalf("service call: %v", err)
			}
			assertCapturedQuery(t, conn, tt.wantQuery, tt.wantArgs)
		})
	}
}

func assertCapturedQuery(t *testing.T, conn *metadataCaptureConn, wantQuery string, wantArgs []driver.Value) {
	t.Helper()
	if conn.query != wantQuery {
		t.Fatalf("query = %q, want %q", conn.query, wantQuery)
	}
	if len(conn.args) != len(wantArgs) {
		t.Fatalf("argument count = %d, want %d", len(conn.args), len(wantArgs))
	}
	for index, want := range wantArgs {
		if conn.args[index].Ordinal != index+1 || conn.args[index].Value != want {
			t.Fatalf("argument[%d] = %#v, want ordinal %d value %#v", index, conn.args[index], index+1, want)
		}
	}
}

type transactionGuardConnector struct {
	conn *transactionGuardConn
}

func (c transactionGuardConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (transactionGuardConnector) Driver() driver.Driver                          { return transactionGuardDriver{} }

type transactionGuardDriver struct{}

func (transactionGuardDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("Open must not be called")
}

type transactionGuardConn struct {
	beginCalled bool
	readOnly    bool
	queryCalled bool
	rolledBack  bool
}

func (*transactionGuardConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("Prepare must not be called")
}
func (*transactionGuardConn) Close() error { return nil }
func (*transactionGuardConn) Begin() (driver.Tx, error) {
	return nil, errors.New("Begin must not be called")
}
func (c *transactionGuardConn) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	c.beginCalled = true
	c.readOnly = options.ReadOnly
	return transactionGuardTx{conn: c}, nil
}
func (c *transactionGuardConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	c.queryCalled = true
	return &transactionGuardRows{}, nil
}

type transactionGuardTx struct {
	conn *transactionGuardConn
}

func (transactionGuardTx) Commit() error { return errors.New("Commit must not be called") }
func (tx transactionGuardTx) Rollback() error {
	tx.conn.rolledBack = true
	return nil
}

type transactionGuardRows struct {
	returned bool
}

func (*transactionGuardRows) Columns() []string { return []string{"value"} }
func (*transactionGuardRows) Close() error      { return nil }
func (r *transactionGuardRows) Next(dest []driver.Value) error {
	if r.returned {
		return io.EOF
	}
	r.returned = true
	dest[0] = int64(1)
	return nil
}

var _ driver.Conn = (*transactionGuardConn)(nil)
var _ driver.ConnBeginTx = (*transactionGuardConn)(nil)
var _ driver.QueryerContext = (*transactionGuardConn)(nil)

type metadataCaptureConnector struct {
	conn *metadataCaptureConn
}

func (c metadataCaptureConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (metadataCaptureConnector) Driver() driver.Driver                          { return metadataCaptureDriver{} }

type metadataCaptureDriver struct{}

func (metadataCaptureDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("Open must not be called")
}

type metadataCaptureConn struct {
	query string
	args  []driver.NamedValue
}

func (*metadataCaptureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("Prepare must not be called")
}
func (*metadataCaptureConn) Close() error { return nil }
func (*metadataCaptureConn) Begin() (driver.Tx, error) {
	return nil, errors.New("Begin must not be called")
}
func (c *metadataCaptureConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.query = query
	c.args = append(c.args[:0], args...)
	return emptyDriverRows{}, nil
}
func (c *metadataCaptureConn) reset() {
	c.query = ""
	c.args = c.args[:0]
}

type emptyDriverRows struct{}

func (emptyDriverRows) Columns() []string         { return nil }
func (emptyDriverRows) Close() error              { return nil }
func (emptyDriverRows) Next([]driver.Value) error { return io.EOF }

var _ driver.Conn = (*metadataCaptureConn)(nil)
var _ driver.QueryerContext = (*metadataCaptureConn)(nil)
