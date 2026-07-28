package logging

import (
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Initialize configures process-wide structured logging.
func Initialize(level string, out io.Writer) error {
	if out == nil {
		out = os.Stderr
	}

	parsed, err := zerolog.ParseLevel(level)
	if err != nil {
		return err
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(parsed)
	log.Logger = zerolog.New(out).With().Timestamp().Logger()
	return nil
}

// Disable silences logs for stdio mode so MCP JSON-RPC messages are not polluted.
func Disable() {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	log.Logger = zerolog.New(io.Discard)
}
