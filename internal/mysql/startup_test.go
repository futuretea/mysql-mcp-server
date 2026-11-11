package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const startupTestDSN = "reader:dsn-sentinel-password@tcp(127.0.0.1:3306)/inventory"

func TestNewServiceRejectsEmptyDSNBeforeConnect(t *testing.T) {
	service, err := NewService(context.Background(), "", false)
	if service != nil {
		t.Fatal("NewService returned a service for an empty DSN")
	}
	assertStartupCode(t, err, CodeConfigurationUnavailable)
}

func TestNewServiceRejectsDangerousPrivileges(t *testing.T) {
	output := captureStartupLog(t)
	for _, privilege := range unsafePrivilegeNames {
		t.Run(privilege, func(t *testing.T) {
			output.Reset()
			conn := newStartupConn([][]string{{
				fmt.Sprintf("GRANT %s ON *.* TO `sentinel-account`@`%%`", privilege),
			}})
			service, err := openStartupService(t, conn, false)
			if service != nil {
				t.Fatal("NewService returned a service for a dangerous account")
			}
			assertStartupCode(t, err, CodeUnsafeServiceRole)
			assertStartupProbe(t, conn, []string{showCurrentGrantsSQL})
			assertNoStartupOptOut(t, output.String())
		})
	}

	t.Run("global ALL PRIVILEGES", func(t *testing.T) {
		output.Reset()
		conn := newStartupConn([][]string{{
			"GRANT ALL PRIVILEGES ON *.* TO `root`@`localhost` WITH GRANT OPTION",
		}})
		service, err := openStartupService(t, conn, false)
		if service != nil {
			t.Fatal("NewService returned a service for global ALL PRIVILEGES")
		}
		assertStartupCode(t, err, CodeUnsafeServiceRole)
		assertNoStartupOptOut(t, output.String())
	})

	t.Run("realistic MySQL 8 root grant", func(t *testing.T) {
		conn := newStartupConn([][]string{{
			"GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, RELOAD, SHUTDOWN, PROCESS, FILE, REFERENCES, INDEX, ALTER, SHOW DATABASES, SUPER, CREATE TEMPORARY TABLES, LOCK TABLES, EXECUTE, REPLICATION SLAVE, REPLICATION CLIENT, CREATE VIEW, SHOW VIEW, CREATE ROUTINE, ALTER ROUTINE, CREATE USER, EVENT, TRIGGER, CREATE TABLESPACE, CREATE ROLE, DROP ROLE ON *.* TO `root`@`localhost` WITH GRANT OPTION",
			"GRANT APPLICATION_PASSWORD_ADMIN,AUDIT_ADMIN,BACKUP_ADMIN,BINLOG_ADMIN,CONNECTION_ADMIN,ROLE_ADMIN,SERVICE_CONNECTION_ADMIN,SESSION_VARIABLES_ADMIN,SET_USER_ID,SYSTEM_USER,SYSTEM_VARIABLES_ADMIN ON *.* TO `root`@`localhost` WITH GRANT OPTION",
		}})
		service, err := openStartupService(t, conn, false)
		if service != nil {
			t.Fatal("NewService returned a service for a root grant set")
		}
		assertStartupCode(t, err, CodeUnsafeServiceRole)
	})

	t.Run("dangerous privilege inherited through a role", func(t *testing.T) {
		conn := newStartupConn([][]string{
			{
				"GRANT USAGE ON *.* TO `reader`@`%`",
				"GRANT `admin`@`%` TO `reader`@`%`",
			},
			{
				"GRANT USAGE ON *.* TO `reader`@`%`",
				"GRANT FILE ON *.* TO `reader`@`%`",
				"GRANT `admin`@`%` TO `reader`@`%`",
			},
		})
		service, err := openStartupService(t, conn, false)
		if service != nil {
			t.Fatal("NewService returned a service for a role with FILE")
		}
		assertStartupCode(t, err, CodeUnsafeServiceRole)
		assertStartupProbe(t, conn, []string{
			showCurrentGrantsSQL,
			"SHOW GRANTS FOR CURRENT_USER() USING `admin`@`%`",
		})
	})

	t.Run("dangerous privilege inherited through a nested role", func(t *testing.T) {
		conn := newStartupConn([][]string{
			{
				"GRANT USAGE ON *.* TO `reader`@`%`",
				"GRANT `admin`@`%` TO `reader`@`%`",
			},
			{
				"GRANT USAGE ON *.* TO `reader`@`%`",
				"GRANT `admin`@`%` TO `reader`@`%`",
				"GRANT `nested`@`%` TO `admin`@`%`",
			},
			{
				"GRANT FILE ON *.* TO `reader`@`%`",
				"GRANT `admin`@`%`, `nested`@`%` TO `reader`@`%`",
			},
		})
		service, err := openStartupService(t, conn, false)
		if service != nil {
			t.Fatal("NewService returned a service for a nested role with FILE")
		}
		assertStartupCode(t, err, CodeUnsafeServiceRole)
		assertStartupProbe(t, conn, []string{
			showCurrentGrantsSQL,
			"SHOW GRANTS FOR CURRENT_USER() USING `admin`@`%`",
			"SHOW GRANTS FOR CURRENT_USER() USING `admin`@`%`, `nested`@`%`",
		})
	})

	t.Run("dangerous privilege inherited through a mandatory role", func(t *testing.T) {
		if showCurrentGrantsSQL != "SHOW GRANTS" {
			t.Fatalf("initial grant probe = %q, want SHOW GRANTS without FOR", showCurrentGrantsSQL)
		}
		conn := newStartupConn([][]string{
			{
				"GRANT USAGE ON *.* TO `reader`@`%`",
				"GRANT `ops`@`%` TO `reader`@`%`",
			},
			{
				"GRANT USAGE ON *.* TO `reader`@`%`",
				"GRANT FILE ON *.* TO `reader`@`%`",
				"GRANT `ops`@`%` TO `reader`@`%`",
			},
		})
		service, err := openStartupService(t, conn, false)
		if service != nil {
			t.Fatal("NewService returned a service for a mandatory role with FILE")
		}
		assertStartupCode(t, err, CodeUnsafeServiceRole)
		assertStartupProbe(t, conn, []string{
			"SHOW GRANTS",
			"SHOW GRANTS FOR CURRENT_USER() USING `ops`@`%`",
		})
	})

	t.Run("roles granted without spaces", func(t *testing.T) {
		conn := newStartupConn([][]string{
			{
				"GRANT USAGE ON *.* TO `u1`@`localhost`",
				"GRANT `r1`@`%`,`r2`@`%` TO `u1`@`localhost`",
			},
			{
				"GRANT USAGE ON *.* TO `u1`@`localhost`",
				"GRANT SHUTDOWN ON *.* TO `u1`@`localhost`",
				"GRANT `r1`@`%`,`r2`@`%` TO `u1`@`localhost`",
			},
		})
		service, err := openStartupService(t, conn, false)
		if service != nil {
			t.Fatal("NewService returned a service for a role list with SHUTDOWN")
		}
		assertStartupCode(t, err, CodeUnsafeServiceRole)
		assertStartupProbe(t, conn, []string{
			"SHOW GRANTS",
			"SHOW GRANTS FOR CURRENT_USER() USING `r1`@`%`, `r2`@`%`",
		})
	})

	t.Run("proxy impersonation", func(t *testing.T) {
		conn := newStartupConn([][]string{{
			"GRANT USAGE ON *.* TO `mcp`@`%`",
			"GRANT PROXY ON `root`@`localhost` TO `mcp`@`%`",
		}})
		service, err := openStartupService(t, conn, false)
		if service != nil {
			t.Fatal("NewService returned a service for a PROXY grant")
		}
		assertStartupCode(t, err, CodeUnsafeServiceRole)
	})

	t.Run("anonymous proxy grant", func(t *testing.T) {
		conn := newStartupConn([][]string{{
			"GRANT PROXY ON ''@'' TO `root`@`localhost` WITH GRANT OPTION",
		}})
		service, err := openStartupService(t, conn, false)
		if service != nil {
			t.Fatal("NewService returned a service for an anonymous PROXY grant")
		}
		assertStartupCode(t, err, CodeUnsafeServiceRole)
	})

	t.Run("dangerous privilege on an account whose name changes length when uppercased", func(t *testing.T) {
		conn := newStartupConn([][]string{{
			"GRANT FILE ON *.* TO `kullanıcı`@`%`",
		}})
		service, err := openStartupService(t, conn, false)
		if service != nil {
			t.Fatal("NewService returned a service for FILE granted to a Unicode account")
		}
		assertStartupCode(t, err, CodeUnsafeServiceRole)
	})
}

func TestNewServiceAllowsUnsafePrivilegesWhenOptedIn(t *testing.T) {
	output := captureStartupLog(t)
	conn := newStartupConn([][]string{{
		"GRANT SUPER, FILE, PROCESS, SHUTDOWN, RELOAD, CREATE USER ON *.* TO `sentinel-account`@`%`",
	}})
	service, err := openStartupService(t, conn, true)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if service == nil {
		t.Fatal("NewService service = nil, want started service")
	}
	t.Cleanup(func() { _ = service.Close() })
	assertStartupProbe(t, conn, []string{showCurrentGrantsSQL})
	if conn.closed != 0 {
		t.Fatalf("startup closed the connection count = %d, want 0", conn.closed)
	}
	text := output.String()
	for _, want := range []string{`"level":"warn"`, startupRoleOptOutMessage} {
		if !strings.Contains(text, want) {
			t.Fatalf("log = %q, want %q", text, want)
		}
	}
	if strings.Contains(text, "dsn-sentinel-password") || strings.Contains(text, "sentinel-account") {
		t.Fatalf("opt-out log leaked account details: %s", text)
	}
}

func TestNewServiceStartsSafeReadOnlyAccountWithoutOptOut(t *testing.T) {
	output := captureStartupLog(t)
	conn := newStartupConn([][]string{{
		"GRANT USAGE ON *.* TO `reader`@`%`",
		"GRANT SELECT ON `inventory`.* TO `reader`@`%`",
	}})
	service, err := openStartupService(t, conn, false)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if service == nil {
		t.Fatal("NewService service = nil, want started service")
	}
	assertStartupProbe(t, conn, []string{showCurrentGrantsSQL})
	assertNoStartupOptOut(t, output.String())
	if conn.closed != 0 {
		t.Fatalf("startup closed the connection count = %d, want 0", conn.closed)
	}

	if err := service.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if conn.closed == 0 {
		t.Fatal("Close did not close the startup connection")
	}
}

func TestNewServiceOptOutDoesNotSkipOtherStartupFailures(t *testing.T) {
	output := captureStartupLog(t)

	t.Run("connection failure", func(t *testing.T) {
		output.Reset()
		conn := newStartupConn(nil)
		conn.beginErr = startupNetError{msg: "dial tcp 127.0.0.1:1: connect: connection refused"}
		service, err := openStartupService(t, conn, true)
		if service != nil {
			t.Fatal("NewService returned a service after the connection check failed")
		}
		assertStartupCode(t, err, CodeConnectionFailed)
		if conn.beginCalls != 1 || len(conn.queries) != 0 {
			t.Fatalf("begin calls=%d queries=%#v, want the connection check and no grant probe", conn.beginCalls, conn.queries)
		}
		assertNoStartupOptOut(t, output.String())
	})

	t.Run("grant probe failure", func(t *testing.T) {
		output.Reset()
		conn := newStartupConn(nil)
		conn.queryErr = errors.New("dsn-sentinel-password in driver text")
		service, err := openStartupService(t, conn, true)
		if service != nil {
			t.Fatal("NewService returned a service after the grant probe failed")
		}
		assertStartupCode(t, err, CodeDatabase)
		if strings.Contains(err.Error(), "dsn-sentinel-password") || strings.Contains(output.String(), "dsn-sentinel-password") {
			t.Fatalf("startup failure leaked driver text: err=%v log=%s", err, output.String())
		}
		assertNoStartupOptOut(t, output.String())
	})
}

func TestNewServiceStartsSafeMandatoryRoleWithoutOptOut(t *testing.T) {
	output := captureStartupLog(t)
	conn := newStartupConn([][]string{
		{
			"GRANT USAGE ON *.* TO `reader`@`%`",
			"GRANT `audit`@`%` TO `reader`@`%`",
		},
		{
			"GRANT USAGE ON *.* TO `reader`@`%`",
			"GRANT SELECT ON `inventory`.* TO `reader`@`%`",
			"GRANT `audit`@`%` TO `reader`@`%`",
		},
	})
	service, err := openStartupService(t, conn, false)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if service == nil {
		t.Fatal("NewService service = nil, want started service")
	}
	t.Cleanup(func() { _ = service.Close() })
	assertStartupProbe(t, conn, []string{
		"SHOW GRANTS",
		"SHOW GRANTS FOR CURRENT_USER() USING `audit`@`%`",
	})
	assertNoStartupOptOut(t, output.String())
}

func TestNewServiceParsesIdentifiersWhoseUppercaseFormChangesLength(t *testing.T) {
	t.Run("column name", func(t *testing.T) {
		conn := newStartupConn([][]string{{
			"GRANT USAGE ON *.* TO `reader`@`%`",
			"GRANT SELECT (`adı`, `şehir`) ON `inventory`.`sample` TO `reader`@`%`",
		}})
		service, err := openStartupService(t, conn, false)
		if err != nil {
			t.Fatalf("NewService: %v", err)
		}
		if service == nil {
			t.Fatal("NewService service = nil, want started service")
		}
		_ = service.Close()
	})

	t.Run("role name", func(t *testing.T) {
		conn := newStartupConn([][]string{
			{
				"GRANT USAGE ON *.* TO `reader`@`%`",
				"GRANT `kullanıcı`@`%` TO `reader`@`%`",
			},
			{
				"GRANT USAGE ON *.* TO `reader`@`%`",
				"GRANT SELECT ON `inventory`.* TO `reader`@`%`",
				"GRANT `kullanıcı`@`%` TO `reader`@`%`",
			},
		})
		service, err := openStartupService(t, conn, false)
		if err != nil {
			t.Fatalf("NewService: %v", err)
		}
		if service == nil {
			t.Fatal("NewService service = nil, want started service")
		}
		t.Cleanup(func() { _ = service.Close() })
		assertStartupProbe(t, conn, []string{
			"SHOW GRANTS",
			"SHOW GRANTS FOR CURRENT_USER() USING `kullanıcı`@`%`",
		})
	})
}

func TestNewServiceAllowsDatabaseLevelPrivilegesWithoutAdministrativeGrants(t *testing.T) {
	conn := newStartupConn([][]string{{
		"GRANT USAGE ON *.* TO `reader`@`%`",
		"GRANT ALL PRIVILEGES ON `inventory`.* TO `reader`@`%`",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON `inventory archive`.* TO `reader`@`%`",
		"GRANT SELECT ON `inventory archive`.* TO `reader`@`%`",
	}})
	service, err := openStartupService(t, conn, false)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if service == nil {
		t.Fatal("NewService service = nil, want started service")
	}
	_ = service.Close()
}

func TestNewServiceRejectsCredentialRoutineAndExecuteGrants(t *testing.T) {
	grants := []string{
		"GRANT SHOW_ROUTINE ON *.* TO `reader`@`%`",
		"GRANT RESOURCE_GROUP_USER ON *.* TO `reader`@`%`",
		"GRANT EXECUTE ON *.* TO `reader`@`%`",
		"GRANT EXECUTE ON FUNCTION `review_db`.`definer_count` TO `reader`@`%`",
		"GRANT SELECT ON *.* TO `reader`@`%`",
		"GRANT ALL PRIVILEGES ON `mysql`.* TO `reader`@`%`",
		"GRANT SELECT (`authentication_string`) ON `mysql`.`user` TO `reader`@`%`",
		"GRANT SELECT ON mysql.user TO `reader`@`%`",
	}
	for _, grant := range grants {
		t.Run(grant, func(t *testing.T) {
			conn := newStartupConn([][]string{{
				"GRANT USAGE ON *.* TO `reader`@`%`",
				grant,
			}})
			service, err := openStartupService(t, conn, false)
			if service != nil {
				t.Fatal("NewService returned a service for a credential, routine, or execute grant")
			}
			assertStartupCode(t, err, CodeUnsafeServiceRole)
		})
	}
}

func TestNewServiceAcceptsQuotedSafeRole(t *testing.T) {
	quotes := []struct {
		name  string
		usage string
		role  string
		read  string
		using string
	}{
		{
			name:  "double",
			usage: `GRANT USAGE ON *.* TO "reader"@"%"`,
			role:  `GRANT "read_role"@"%" TO "reader"@"%"`,
			read:  `GRANT SELECT ON "inventory".* TO "reader"@"%"`,
			using: `SHOW GRANTS FOR CURRENT_USER() USING "read_role"@"%"`,
		},
		{
			name:  "single",
			usage: `GRANT USAGE ON *.* TO 'reader'@'%'`,
			role:  `GRANT 'read_role'@'%' TO 'reader'@'%'`,
			read:  `GRANT SELECT ON 'inventory'.* TO 'reader'@'%'`,
			using: `SHOW GRANTS FOR CURRENT_USER() USING 'read_role'@'%'`,
		},
	}
	for _, quote := range quotes {
		t.Run(quote.name, func(t *testing.T) {
			conn := newStartupConn([][]string{
				{quote.usage, quote.role},
				{quote.usage, quote.read, quote.role},
			})
			service, err := openStartupService(t, conn, false)
			if err != nil {
				t.Fatalf("NewService: %v", err)
			}
			if service == nil {
				t.Fatal("NewService service = nil, want started service")
			}
			t.Cleanup(func() { _ = service.Close() })
			assertStartupProbe(t, conn, []string{showCurrentGrantsSQL, quote.using})
		})
	}
}

func TestNewServiceBoundsStartupAndIgnoresOptOut(t *testing.T) {
	previous := startupTimeout
	startupTimeout = 200 * time.Millisecond
	t.Cleanup(func() { startupTimeout = previous })

	conn := newStartupConn(nil)
	conn.blockOnContext = true
	start := time.Now()
	service, err := openStartupService(t, conn, true)
	if service != nil {
		t.Fatal("NewService returned a service after the startup deadline")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("startup took %s", time.Since(start))
	}
	assertStartupCode(t, err, CodeConnectionFailed)
}

func TestNewServiceBoundsBlackholeDial(t *testing.T) {
	previous := startupTimeout
	startupTimeout = 300 * time.Millisecond
	t.Cleanup(func() { startupTimeout = previous })

	start := time.Now()
	service, err := NewService(context.Background(), "reader:pw@tcp(192.0.2.1:3306)/", true)
	if service != nil {
		t.Fatal("NewService returned a service for a blackholed address")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("dial took %s", elapsed)
	}
	assertStartupCode(t, err, CodeConnectionFailed)
}

func TestNewServiceRejectsDoubleQuotedDangerousRole(t *testing.T) {
	conn := newStartupConn([][]string{
		{
			`GRANT USAGE ON *.* TO "reader"@"%"`,
			`GRANT "file_role"@"%" TO "reader"@"%"`,
		},
		{
			`GRANT FILE ON *.* TO "reader"@"%"`,
			`GRANT "file_role"@"%" TO "reader"@"%"`,
		},
	})
	service, err := openStartupService(t, conn, false)
	if service != nil {
		t.Fatal("NewService returned a service for a double-quoted FILE role")
	}
	assertStartupCode(t, err, CodeUnsafeServiceRole)
}

func openStartupService(t *testing.T, conn *startupConn, allowUnsafe bool) (*Service, error) {
	t.Helper()
	return newService(context.Background(), startupTestDSN, allowUnsafe, func(string) (*sql.DB, error) {
		return sql.OpenDB(startupConnector{conn: conn}), nil
	})
}

func assertStartupCode(t *testing.T, err error, want ErrorCode) {
	t.Helper()
	code, ok := CodeOf(err)
	if !ok || code != want {
		t.Fatalf("CodeOf(err) = %q, %t; want %s, true", code, ok, want)
	}
	if err.Error() != string(want) {
		t.Fatalf("error = %q, want code %s", err.Error(), want)
	}
	if strings.Contains(err.Error(), "dsn-sentinel-password") {
		t.Fatalf("error leaked the DSN password: %v", err)
	}
}

func assertStartupProbe(t *testing.T, conn *startupConn, wantQueries []string) {
	t.Helper()
	if conn.beginCalls != 1 || !conn.readOnly {
		t.Fatalf("startup begin calls=%d readOnly=%t, want 1/true", conn.beginCalls, conn.readOnly)
	}
	if conn.rollbacks == 0 {
		t.Fatal("startup transaction was not rolled back")
	}
	if strings.Join(conn.queries, "\n") != strings.Join(wantQueries, "\n") {
		t.Fatalf("startup queries = %#v, want %#v", conn.queries, wantQueries)
	}
}

func assertNoStartupOptOut(t *testing.T, text string) {
	t.Helper()
	if strings.Contains(text, startupRoleOptOutMessage) || strings.Contains(text, "dsn-sentinel-password") || strings.Contains(text, "sentinel-account") {
		t.Fatalf("startup log = %q, want no opt-out warning or account details", text)
	}
}

func captureStartupLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previousLogger := log.Logger
	previousLevel := zerolog.GlobalLevel()
	log.Logger = zerolog.New(&output)
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})
	return &output
}

func newStartupConn(grantSets [][]string) *startupConn {
	copied := make([][]string, len(grantSets))
	for i, grants := range grantSets {
		copied[i] = append([]string(nil), grants...)
	}
	return &startupConn{grantSets: copied}
}

type startupNetError struct {
	msg string
}

func (e startupNetError) Error() string   { return e.msg }
func (e startupNetError) Timeout() bool   { return false }
func (e startupNetError) Temporary() bool { return true }

type startupConnector struct {
	conn *startupConn
}

func (c startupConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (startupConnector) Driver() driver.Driver                          { return startupDriver{} }

type startupDriver struct{}

func (startupDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("Open must not be called")
}

type startupConn struct {
	grantSets      [][]string
	queries        []string
	beginCalls     int
	rollbacks      int
	closed         int
	readOnly       bool
	blockOnContext bool
	beginErr       error
	queryErr       error
}

func (*startupConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("Prepare must not be called")
}
func (c *startupConn) Close() error { c.closed++; return nil }
func (*startupConn) Begin() (driver.Tx, error) {
	return nil, errors.New("Begin must not be called")
}
func (c *startupConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.beginCalls++
	c.readOnly = opts.ReadOnly
	if c.blockOnContext {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return startupTx{conn: c}, nil
}
func (c *startupConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.queries = append(c.queries, query)
	if c.queryErr != nil {
		return nil, c.queryErr
	}
	if len(c.grantSets) == 0 {
		return nil, errors.New("unexpected startup query")
	}
	grants := c.grantSets[0]
	c.grantSets = c.grantSets[1:]
	return &startupRows{values: grants}, nil
}

type startupTx struct {
	conn *startupConn
}

func (startupTx) Commit() error { return errors.New("Commit must not be called") }
func (tx startupTx) Rollback() error {
	tx.conn.rollbacks++
	return nil
}

type startupRows struct {
	values []string
	index  int
}

func (*startupRows) Columns() []string { return []string{"Grants"} }
func (*startupRows) Close() error      { return nil }
func (r *startupRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	dest[0] = r.values[r.index]
	r.index++
	return nil
}

var _ driver.Conn = (*startupConn)(nil)
var _ driver.ConnBeginTx = (*startupConn)(nil)
var _ driver.QueryerContext = (*startupConn)(nil)
