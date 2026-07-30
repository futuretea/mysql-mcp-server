package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestMySQL8046Integration(t *testing.T) {
	if os.Getenv("MYSQL_MCP_INTEGRATION") != "1" {
		t.Skip("set MYSQL_MCP_INTEGRATION=1 to run the disposable mysql:8.0.46 integration test")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker is unavailable; unit tests provide the declared substitute verification")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	port := startMySQL8046Container(t, ctx)
	dsn := integrationRuntimeDSN(t, port)
	db := openIntegrationDatabase(t, dsn)
	seedIntegrationDatabase(t, ctx, db)
	service := openIntegrationService(t, dsn)

	assertServerDiscovery(t, ctx, service)
	assertQueryResult(t, ctx, service)
	assertSystemViewsAreExcluded(t, ctx, db, service)
	assertReadOnlyTransactionRejectsWrites(t, ctx, db)
	assertExternalEntryPoints(t, ctx, dsn)
}

func startMySQL8046Container(t *testing.T, ctx context.Context) string {
	t.Helper()
	name := fmt.Sprintf("mysql-mcp-test-%d", time.Now().UnixNano())
	command := exec.CommandContext(ctx, "docker", "run", "-d", "--rm", "--name", name,
		"-e", "MYSQL_ALLOW_EMPTY_PASSWORD=yes", "-e", "MYSQL_DATABASE=mysql_mcp_test",
		"-p", "127.0.0.1::3306", "mysql:8.0.46")
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("Docker could not start mysql:8.0.46: %s", strings.TrimSpace(string(output)))
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	if !waitForMySQL(ctx, name) {
		t.Fatal("mysql:8.0.46 did not become ready")
	}
	port, err := dockerPort(ctx, name)
	if err != nil {
		t.Fatalf("inspect dynamic MySQL port: %v", err)
	}
	return port
}

func integrationRuntimeDSN(t *testing.T, port string) string {
	t.Helper()
	rawConfig := mysqldriver.NewConfig()
	rawConfig.User = "root"
	rawConfig.Net = "tcp"
	rawConfig.Addr = "127.0.0.1:" + port
	rawConfig.DBName = "mysql_mcp_test"
	rawConfig.ParseTime = false
	return rawConfig.FormatDSN()
}

func openIntegrationDatabase(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open fixture database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedIntegrationDatabase(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `CREATE TABLE sample (
id INT PRIMARY KEY,
name VARCHAR(64) NOT NULL,
binary_value BINARY(1) NULL,
unsigned_value BIGINT UNSIGNED NOT NULL,
float_value FLOAT NOT NULL,
double_value DOUBLE NOT NULL,
decimal_value DECIMAL(10,2) NOT NULL,
created_at DATETIME NOT NULL
)`); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE VIEW sample_view AS SELECT id, name FROM sample"); err != nil {
		t.Fatalf("create fixture view: %v", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE INDEX sample_name_index ON sample (name)"); err != nil {
		t.Fatalf("create fixture index: %v", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE INDEX sample_lower_name_index ON sample ((LOWER(name)))"); err != nil {
		t.Fatalf("create fixture functional index: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO sample (id, name, binary_value, unsigned_value, float_value, double_value, decimal_value, created_at) VALUES (1, 'hello', X'FF', 18446744073709551615, 1.5, 2.25, 3.50, '2026-01-02 03:04:05')"); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}
}

func openIntegrationService(t *testing.T, dsn string) *Service {
	t.Helper()
	service, err := NewService(dsn)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return service
}

func assertServerDiscovery(t *testing.T, ctx context.Context, service *Service) {
	t.Helper()
	serverInfo, err := service.GetServerInfo(ctx)
	if err != nil || !strings.HasPrefix(serverInfo.Version, "8.0.46") || serverInfo.CurrentDatabase == nil || *serverInfo.CurrentDatabase != "mysql_mcp_test" {
		t.Fatalf("GetServerInfo() = %+v, %v", serverInfo, err)
	}
	databases, err := service.ListDatabases(ctx)
	if err != nil || !containsString(databases, "mysql_mcp_test") {
		t.Fatalf("ListDatabases() = %v, %v", databases, err)
	}
	tables, err := service.ListTables(ctx, "mysql_mcp_test")
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if !containsTable(tables.Tables, "sample", "BASE TABLE") || !containsTable(tables.Tables, "sample_view", "VIEW") {
		t.Fatalf("ListTables() = %+v, want sample table and view", tables.Tables)
	}
	columns, err := service.DescribeTable(ctx, "mysql_mcp_test", "sample")
	if err != nil || len(columns.Columns) != 8 {
		t.Fatalf("DescribeTable() = %+v, %v", columns, err)
	}
	indexes, err := service.ListIndexes(ctx, "mysql_mcp_test", "sample")
	if err != nil || !hasIndex(indexes.Indexes, "sample_name_index") || !hasExpressionIndex(indexes.Indexes, "sample_lower_name_index") {
		t.Fatalf("ListIndexes() = %+v, %v", indexes, err)
	}
}

func assertQueryResult(t *testing.T, ctx context.Context, service *Service) {
	t.Helper()
	result, err := service.Query(ctx, "SELECT id, name, binary_value, unsigned_value, float_value, double_value, decimal_value, created_at FROM sample")
	if err != nil || len(result.Rows) != 1 || len(result.Columns) != 8 {
		t.Fatalf("Query() = %+v, %v", result, err)
	}
	for index, want := range []Cell{
		{Encoding: "integer", Value: "1"},
		{Encoding: "utf8", Value: "hello"},
		{Encoding: "base64", Value: "/w=="},
		{Encoding: "integer", Value: "18446744073709551615"},
		{Encoding: "float", Value: "1.5"},
		{Encoding: "float", Value: "2.25"},
		{Encoding: "utf8", Value: "3.50"},
		{Encoding: "utf8", Value: "2026-01-02 03:04:05"},
	} {
		got, ok := result.Rows[0][index].(Cell)
		if !ok || got != want {
			t.Fatalf("result cell[%d] = %#v, want %#v", index, result.Rows[0][index], want)
		}
	}
}

func assertSystemViewsAreExcluded(t *testing.T, ctx context.Context, db *sql.DB, service *Service) {
	t.Helper()
	var systemView string
	err := db.QueryRowContext(ctx, "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = 'information_schema' AND TABLE_TYPE = 'SYSTEM VIEW' LIMIT 1").Scan(&systemView)
	if err != nil {
		t.Fatalf("find system view fixture: %v", err)
	}
	systemTables, err := service.ListTables(ctx, "information_schema")
	if err != nil {
		t.Fatalf("ListTables(information_schema): %v", err)
	}
	if containsTableName(systemTables.Tables, systemView) {
		t.Fatalf("ListTables returned system view %q", systemView)
	}
}

func assertReadOnlyTransactionRejectsWrites(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin read-only transaction: %v", err)
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO sample (id, name, unsigned_value, float_value, double_value, decimal_value, created_at) VALUES (2, 'blocked', 2, 1, 1, 1, '2026-01-02 03:04:05')")
	_ = tx.Rollback()
	var mysqlError *mysqldriver.MySQLError
	if !errors.As(err, &mysqlError) || mysqlError.Number != 1792 {
		t.Fatalf("read-only write error = %v, want ER_CANT_EXECUTE_IN_READ_ONLY_TRANSACTION", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sample WHERE id = 2").Scan(&count); err != nil || count != 0 {
		t.Fatalf("blocked write persisted count=%d err=%v", count, err)
	}
}

func waitForMySQL(ctx context.Context, name string) bool {
	for ctx.Err() == nil {
		if exec.CommandContext(ctx, "docker", "exec", name, "mysqladmin", "--protocol=tcp", "-uroot", "ping").Run() == nil {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func dockerPort(ctx context.Context, name string) (string, error) {
	output, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{(index (index .NetworkSettings.Ports \"3306/tcp\") 0).HostPort}}", name).Output()
	if err != nil {
		return "", err
	}
	port := strings.TrimSpace(string(output))
	if _, err := strconv.Atoi(port); err != nil {
		return "", err
	}
	return port, nil
}

func containsTable(tables []Table, name, tableType string) bool {
	for _, table := range tables {
		if table.Name == name && table.Type == tableType {
			return true
		}
	}
	return false
}

func hasIndex(indexes []Index, name string) bool {
	for _, index := range indexes {
		if index.Name == name {
			return true
		}
	}
	return false
}

func hasExpressionIndex(indexes []Index, name string) bool {
	for _, index := range indexes {
		if index.Name == name && len(index.KeyParts) == 1 && index.KeyParts[0].ColumnName == nil && index.KeyParts[0].Expression != nil {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsTableName(tables []Table, name string) bool {
	for _, table := range tables {
		if table.Name == name {
			return true
		}
	}
	return false
}
