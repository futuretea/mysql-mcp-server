package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/futuretea/mysql-mcp-server/pkg/core/config"
)

func TestInitializeServerLoggingStdioRoutesDatabaseDiagnosticsToStderr(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	previousLogger := log.Logger
	previousLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})

	err := initializeServerLogging(&config.StaticConfig{Port: 0, LogLevel: "info"}, IOStreams{
		Out:    &stdout,
		ErrOut: &stderr,
	})
	if err != nil {
		t.Fatalf("initializeServerLogging: %v", err)
	}
	log.Error().Str("operation", "query.execute").Uint16("mysql_error_number", 1142).Msg("MySQL operation failed")

	if got := stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want no diagnostics", got)
	}
	for _, want := range []string{"MySQL operation failed", `"operation":"query.execute"`, `"mysql_error_number":1142`} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}
