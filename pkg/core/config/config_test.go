package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/spf13/viper"

	"github.com/futuretea/mysql-mcp-server/pkg/core/config"
)

func TestLoadConfigDefaults(t *testing.T) {
	clearStaticConfigEnv(t)

	cfg, err := config.LoadConfig("", viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Port != 0 {
		t.Fatalf("expected port 0, got %d", cfg.Port)
	}
	if cfg.Listen != "127.0.0.1" {
		t.Fatalf("expected listen 127.0.0.1, got %q", cfg.Listen)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("expected log_level info, got %q", cfg.LogLevel)
	}
	if cfg.AllowUnsafeDatabaseRole {
		t.Fatal("AllowUnsafeDatabaseRole = true, want false")
	}
}

func TestLoadConfigUsesMySQLMCPEnvironmentPrefix(t *testing.T) {
	clearStaticConfigEnv(t)

	t.Setenv("MYSQL_MCP_PORT", "8123")
	t.Setenv("MYSQL_MCP_LISTEN", "0.0.0.0")
	t.Setenv("MYSQL_MCP_LOG_LEVEL", "DEBUG")
	t.Setenv("MYSQL_MCP_SSE_BASE_URL", "https://mcp.example.test/events")
	t.Setenv("MCP_PORT", "4321")
	t.Setenv("MCP_LISTEN", "127.0.0.2")
	t.Setenv("MCP_LOG_LEVEL", "error")
	t.Setenv("MCP_SSE_BASE_URL", "https://legacy.example.test/events")

	cfg, err := config.LoadConfig("", viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Port != 8123 {
		t.Fatalf("Port = %d, want 8123", cfg.Port)
	}
	if cfg.Listen != "0.0.0.0" {
		t.Fatalf("Listen = %q, want 0.0.0.0", cfg.Listen)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("LogLevel = %q, want debug", cfg.LogLevel)
	}
	if cfg.SSEBaseURL != "https://mcp.example.test/events" {
		t.Fatalf("SSEBaseURL = %q, want new prefix value", cfg.SSEBaseURL)
	}
}

func TestLoadConfigIgnoresLegacyMCPEnvironmentPrefix(t *testing.T) {
	clearStaticConfigEnv(t)

	t.Setenv("MCP_PORT", "8123")
	t.Setenv("MCP_LISTEN", "0.0.0.0")
	t.Setenv("MCP_LOG_LEVEL", "debug")
	t.Setenv("MCP_SSE_BASE_URL", "https://legacy.example.test/events")

	cfg, err := config.LoadConfig("", viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Port != 0 {
		t.Fatalf("Port = %d, want default 0", cfg.Port)
	}
	if cfg.Listen != "127.0.0.1" {
		t.Fatalf("Listen = %q, want default 127.0.0.1", cfg.Listen)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want default info", cfg.LogLevel)
	}
	if cfg.SSEBaseURL != "" {
		t.Fatalf("SSEBaseURL = %q, want default empty value", cfg.SSEBaseURL)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.StaticConfig
		wantSub string
	}{
		{
			name:    "bad port",
			cfg:     config.StaticConfig{Port: 70000, LogLevel: "info", Listen: "127.0.0.1"},
			wantSub: "port must be between",
		},
		{
			name:    "empty listen",
			cfg:     config.StaticConfig{Port: 8080, LogLevel: "info", Listen: " "},
			wantSub: "listen must be set",
		},
		{
			name:    "bad log level",
			cfg:     config.StaticConfig{Port: 0, LogLevel: "nope", Listen: "127.0.0.1"},
			wantSub: "invalid log_level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.wantSub)
			}
		})
	}
}

func TestGetListenAddress(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.StaticConfig
		want string
	}{
		{name: "stdio", cfg: config.StaticConfig{Port: 0, Listen: "127.0.0.1"}, want: ""},
		{name: "IPv4", cfg: config.StaticConfig{Port: 8080, Listen: "127.0.0.1"}, want: "127.0.0.1:8080"},
		{name: "hostname", cfg: config.StaticConfig{Port: 8080, Listen: "localhost"}, want: "localhost:8080"},
		{name: "IPv6", cfg: config.StaticConfig{Port: 8080, Listen: "::1"}, want: "[::1]:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetListenAddress(); got != tt.want {
				t.Fatalf("GetListenAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadConfigAllowUnsafeDatabaseRoleFromEnvironmentAndFile(t *testing.T) {
	clearStaticConfigEnv(t)
	if err := os.Unsetenv("MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE"); err != nil {
		t.Fatalf("unset allow_unsafe_database_role: %v", err)
	}

	t.Setenv("MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE", "true")
	cfg, err := config.LoadConfig("", viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.AllowUnsafeDatabaseRole {
		t.Fatal("AllowUnsafeDatabaseRole = false, want true from MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE")
	}

	t.Setenv("MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE", "false")
	cfg, err = config.LoadConfig("", viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AllowUnsafeDatabaseRole {
		t.Fatal("AllowUnsafeDatabaseRole = true, want false")
	}

	if err := os.Unsetenv("MYSQL_MCP_ALLOW_UNSAFE_DATABASE_ROLE"); err != nil {
		t.Fatalf("unset allow_unsafe_database_role: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte("allow_unsafe_database_role: true\nlog_level: info\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err = config.LoadConfig(path, viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.AllowUnsafeDatabaseRole {
		t.Fatal("AllowUnsafeDatabaseRole = false, want true from config file")
	}
}

func TestLoadMySQLRuntimeConfigBuildsTCPDSNFromFields(t *testing.T) {
	runtimeConfig, err := config.LoadMySQLRuntimeConfig(runtimeGetenv(map[string]string{
		"MYSQL_MCP_HOST":     " db.internal ",
		"MYSQL_MCP_DB_PORT":  "3307",
		"MYSQL_MCP_USERNAME": " reader ",
		"MYSQL_MCP_PASSWORD": " password with spaces ",
		"MYSQL_MCP_DATABASE": " inventory ",
	}))
	if err != nil {
		t.Fatalf("LoadMySQLRuntimeConfig: %v", err)
	}

	parsed := parseRuntimeDSN(t, runtimeConfig.DSN)
	if parsed.Net != "tcp" {
		t.Fatalf("DSN network = %q, want tcp", parsed.Net)
	}
	if parsed.Addr != "db.internal:3307" {
		t.Fatalf("DSN address = %q, want db.internal:3307", parsed.Addr)
	}
	if parsed.User != "reader" {
		t.Fatalf("DSN user = %q, want reader", parsed.User)
	}
	if parsed.Passwd != " password with spaces " {
		t.Fatal("DSN password was changed")
	}
	if parsed.DBName != "inventory" {
		t.Fatalf("DSN database = %q, want inventory", parsed.DBName)
	}
	if parsed.ParseTime {
		t.Fatal("DSN ParseTime = true, want false")
	}
}

func TestLoadMySQLRuntimeConfigDefaultsHostAndDBPort(t *testing.T) {
	runtimeConfig, err := config.LoadMySQLRuntimeConfig(runtimeGetenv(map[string]string{
		"MYSQL_MCP_USERNAME": "reader",
	}))
	if err != nil {
		t.Fatalf("LoadMySQLRuntimeConfig: %v", err)
	}

	parsed := parseRuntimeDSN(t, runtimeConfig.DSN)
	if parsed.Net != "tcp" {
		t.Fatalf("DSN network = %q, want tcp", parsed.Net)
	}
	if parsed.Addr != "127.0.0.1:3306" {
		t.Fatalf("DSN address = %q, want 127.0.0.1:3306", parsed.Addr)
	}
	if parsed.User != "reader" {
		t.Fatalf("DSN user = %q, want reader", parsed.User)
	}
	if parsed.Passwd != "" {
		t.Fatal("DSN password = non-empty, want empty")
	}
	if parsed.DBName != "" {
		t.Fatalf("DSN database = %q, want empty", parsed.DBName)
	}
	if parsed.ParseTime {
		t.Fatal("DSN ParseTime = true, want false")
	}
}

func TestLoadMySQLRuntimeConfigAcceptsDBPortBoundaries(t *testing.T) {
	for _, dbPort := range []string{"1", "65535"} {
		t.Run(dbPort, func(t *testing.T) {
			runtimeConfig, err := config.LoadMySQLRuntimeConfig(runtimeGetenv(map[string]string{
				"MYSQL_MCP_DB_PORT":  dbPort,
				"MYSQL_MCP_USERNAME": "reader",
			}))
			if err != nil {
				t.Fatalf("LoadMySQLRuntimeConfig: %v", err)
			}

			parsed := parseRuntimeDSN(t, runtimeConfig.DSN)
			if got, want := parsed.Addr, "127.0.0.1:"+dbPort; got != want {
				t.Fatalf("DSN address = %q, want %q", got, want)
			}
		})
	}
}

func TestLoadMySQLRuntimeConfigRejectsBlankUsernameWithoutLeakingPassword(t *testing.T) {
	const password = "do-not-echo-this-password"

	_, err := config.LoadMySQLRuntimeConfig(runtimeGetenv(map[string]string{
		"MYSQL_MCP_USERNAME": " \t ",
		"MYSQL_MCP_PASSWORD": password,
	}))
	if err == nil {
		t.Fatal("LoadMySQLRuntimeConfig() error = nil, want missing username error")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatal("LoadMySQLRuntimeConfig() error leaked the password")
	}
}

func TestLoadMySQLRuntimeConfigRejectsInvalidDBPortWithoutLeakingPassword(t *testing.T) {
	const password = "do-not-echo-this-password"

	for _, dbPort := range []string{"0", "-1", "65536", "not-a-port"} {
		t.Run(dbPort, func(t *testing.T) {
			_, err := config.LoadMySQLRuntimeConfig(runtimeGetenv(map[string]string{
				"MYSQL_MCP_DB_PORT":  dbPort,
				"MYSQL_MCP_USERNAME": "reader",
				"MYSQL_MCP_PASSWORD": password,
			}))
			if err == nil {
				t.Fatal("LoadMySQLRuntimeConfig() error = nil, want invalid DB port error")
			}
			if strings.Contains(err.Error(), password) {
				t.Fatal("LoadMySQLRuntimeConfig() error leaked the password")
			}
			if strings.Contains(err.Error(), dbPort) {
				t.Fatalf("LoadMySQLRuntimeConfig() error leaked DB port %q", dbPort)
			}
		})
	}
}

func TestLoadMySQLRuntimeConfigIgnoresLegacyDSN(t *testing.T) {
	const password = "legacy-password"

	_, err := config.LoadMySQLRuntimeConfig(runtimeGetenv(map[string]string{
		"MYSQL_DSN": "reader:" + password + "@tcp(127.0.0.1:3306)/inventory",
	}))
	if err == nil {
		t.Fatal("LoadMySQLRuntimeConfig() error = nil, want missing username error")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatal("LoadMySQLRuntimeConfig() error leaked the password")
	}
}

func runtimeGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func clearStaticConfigEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"PORT",
		"LISTEN",
		"SSE_BASE_URL",
		"LOG_LEVEL",
		"ENABLED_TOOLS",
		"DISABLED_TOOLS",
		"ENABLED_DOMAINS",
		"DISABLED_DOMAINS",
		"ALLOW_UNSAFE_DATABASE_ROLE",
	} {
		t.Setenv("MYSQL_MCP_"+key, "")
		t.Setenv("MCP_"+key, "")
	}
}

func parseRuntimeDSN(t *testing.T, dsn string) *mysqldriver.Config {
	t.Helper()

	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN(%q): %v", dsn, err)
	}
	return parsed
}
