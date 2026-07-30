package logging

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func TestInitializeStdioLogsErrorsOnly(t *testing.T) {
	var output bytes.Buffer
	previousLogger := log.Logger
	previousLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})

	if err := InitializeStdio("debug", &output); err != nil {
		t.Fatalf("InitializeStdio: %v", err)
	}
	log.Info().Msg("must not be logged")
	log.Error().Msg("must be logged")

	text := output.String()
	if strings.Contains(text, "must not be logged") {
		t.Fatalf("stdio log included info event: %q", text)
	}
	if !strings.Contains(text, "must be logged") {
		t.Fatalf("stdio log omitted error event: %q", text)
	}
}
