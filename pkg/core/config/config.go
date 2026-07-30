package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

// MySQLRuntimeConfig contains the runtime-only MySQL connection setting.
type MySQLRuntimeConfig struct {
	DSN string
}

// LoadMySQLRuntimeConfig loads MySQL connection fields from dedicated runtime
// environment variables. The generated DSN is not part of the static Viper config.
func LoadMySQLRuntimeConfig(getenv func(string) string) (MySQLRuntimeConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	host := strings.TrimSpace(getenv("MYSQL_MCP_HOST"))
	if host == "" {
		host = "127.0.0.1"
	}

	port := strings.TrimSpace(getenv("MYSQL_MCP_DB_PORT"))
	if port == "" {
		port = "3306"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return MySQLRuntimeConfig{}, fmt.Errorf("MYSQL_MCP_DB_PORT is invalid")
	}

	username := strings.TrimSpace(getenv("MYSQL_MCP_USERNAME"))
	if username == "" {
		return MySQLRuntimeConfig{}, fmt.Errorf("MYSQL_MCP_USERNAME is required")
	}

	connection := mysqldriver.NewConfig()
	connection.User = username
	connection.Passwd = getenv("MYSQL_MCP_PASSWORD")
	connection.Net = "tcp"
	connection.Addr = net.JoinHostPort(host, strconv.Itoa(portNumber))
	connection.DBName = strings.TrimSpace(getenv("MYSQL_MCP_DATABASE"))
	connection.ParseTime = false

	return MySQLRuntimeConfig{DSN: connection.FormatDSN()}, nil
}

// StaticConfig contains configuration that is fixed when the server starts.
type StaticConfig struct {
	Port            int      `mapstructure:"port"`
	Listen          string   `mapstructure:"listen"`
	SSEBaseURL      string   `mapstructure:"sse_base_url"`
	LogLevel        string   `mapstructure:"log_level"`
	EnabledTools    []string `mapstructure:"enabled_tools"`
	DisabledTools   []string `mapstructure:"disabled_tools"`
	EnabledDomains  []string `mapstructure:"enabled_domains"`
	DisabledDomains []string `mapstructure:"disabled_domains"`
}

// Validate checks whether the configuration can be used to start the server.
func (c *StaticConfig) Validate() error {
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535, got %d", c.Port)
	}
	if c.Port != 0 && strings.TrimSpace(c.Listen) == "" {
		return fmt.Errorf("listen must be set when port is non-zero")
	}
	if _, err := zerolog.ParseLevel(c.LogLevel); err != nil {
		return fmt.Errorf("invalid log_level %q: %w", c.LogLevel, err)
	}
	return nil
}

// GetListenAddress returns the configured HTTP listen address (host:port).
func (c *StaticConfig) GetListenAddress() string {
	if c.Port == 0 {
		return ""
	}
	return net.JoinHostPort(c.Listen, strconv.Itoa(c.Port))
}

// LoadConfig loads configuration from defaults, optional YAML, environment, and flags.
func LoadConfig(configPath string, v *viper.Viper) (*StaticConfig, error) {
	if v == nil {
		v = viper.New()
	}

	defaults := map[string]any{
		"port":             0,
		"listen":           "127.0.0.1",
		"sse_base_url":     "",
		"log_level":        "info",
		"enabled_tools":    []string{},
		"disabled_tools":   []string{},
		"enabled_domains":  []string{},
		"disabled_domains": []string{},
	}
	for key, value := range defaults {
		v.SetDefault(key, value)
	}

	if configPath != "" {
		v.SetConfigFile(configPath)
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}

	v.SetEnvPrefix("MYSQL_MCP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	cfg := &StaticConfig{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.Listen = strings.TrimSpace(cfg.Listen)
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	cfg.EnabledTools = normalizeStringSlice(cfg.EnabledTools)
	cfg.DisabledTools = normalizeStringSlice(cfg.DisabledTools)
	cfg.EnabledDomains = normalizeStringSlice(cfg.EnabledDomains)
	cfg.DisabledDomains = normalizeStringSlice(cfg.DisabledDomains)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func normalizeStringSlice(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			normalized = append(normalized, value)
		}
	}
	return normalized
}
