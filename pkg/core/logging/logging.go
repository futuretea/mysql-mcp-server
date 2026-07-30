package logging

import (
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Initialize configures process-wide structured logging.
func Initialize(level string, out io.Writer) error {
	parsed, err := zerolog.ParseLevel(level)
	if err != nil {
		return err
	}
	return initialize(parsed, out)
}

// InitializeStdio writes only errors to stderr so MCP JSON-RPC output remains clean.
func InitializeStdio(level string, out io.Writer) error {
	parsed, err := zerolog.ParseLevel(level)
	if err != nil {
		return err
	}
	if parsed < zerolog.ErrorLevel {
		parsed = zerolog.ErrorLevel
	}
	return initialize(parsed, out)
}

func initialize(level zerolog.Level, out io.Writer) error {
	if out == nil {
		out = os.Stderr
	}
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(level)
	log.Logger = zerolog.New(out).With().Timestamp().Logger()
	return nil
}

// Disable silences logs for stdio mode so MCP JSON-RPC messages are not polluted.
func Disable() {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	log.Logger = zerolog.New(io.Discard)
}
