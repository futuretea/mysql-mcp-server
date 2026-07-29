package config_test

import (
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/futuretea/mysql-mcp-server/pkg/core/config"
)

func TestLoadConfigDefaults(t *testing.T) {
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

func TestLoadMySQLRuntimeConfigRejectsMissingDSN(t *testing.T) {

	_, err := config.LoadMySQLRuntimeConfig(func(string) string { return "" })
	if err == nil {
		t.Fatal("LoadMySQLRuntimeConfig() error = nil, want missing DSN error")
	}
	if strings.Contains(err.Error(), "MYSQL_DSN=") {
		t.Fatalf("missing DSN error leaked configuration detail: %q", err)
	}
}

func TestParseMySQLRuntimeConfigForcesParseTimeFalse(t *testing.T) {
	input := "reader@tcp(127.0.0.1:3306)/inventory?parseTime=true"

	cfg, err := config.ParseMySQLRuntimeConfig(input)
	if err != nil {
		t.Fatalf("ParseMySQLRuntimeConfig: %v", err)
	}
	if strings.Contains(cfg.DSN, "parseTime=true") {
		t.Fatalf("DSN = %q, want parseTime=false", cfg.DSN)
	}
}

func TestParseMySQLRuntimeConfigDoesNotEchoInvalidDSN(t *testing.T) {
	input := "not a valid dsn"
	_, err := config.ParseMySQLRuntimeConfig(input)
	if err == nil {
		t.Fatal("ParseMySQLRuntimeConfig() error = nil, want error")
	}
	if strings.Contains(err.Error(), input) {
		t.Fatalf("invalid DSN error leaked input: %q", err)
	}
}
