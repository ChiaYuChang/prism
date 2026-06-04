package obs

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggerConfig_NoOTELHeaderLeak(t *testing.T) {
	const header = "bearer-abcdef-0123456789"
	cfg := LoggerConfig{
		Level: "info",
		OTEL: OTELLoggerConfig{
			Enable: true,
			URL:    "otel-collector:4317",
			Headers: map[string]string{
				"authorization": header,
			},
		},
	}

	for _, verb := range []string{"%v", "%+v"} {
		out := fmt.Sprintf(verb, cfg)
		assert.NotContains(t, out, header, "verb %q leaked OTEL header", verb)
	}

	var buf strings.Builder
	h := slog.NewTextHandler(&buf, nil)
	slog.New(h).Info("logger", slog.Any("config", cfg))
	assert.NotContains(t, buf.String(), header)
}

func TestInitConfiguredLogger_FileOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prism.log")
	logger, file, shutdown, err := InitConfiguredLogger(context.Background(), LoggerConfig{
		Level: "info",
		File:  FileLoggerConfig{Enable: true, File: path},
	})
	require.NoError(t, err)
	require.NotNil(t, file)
	defer func() { _ = file.Close() }()
	defer func() { _ = shutdown(context.Background()) }()

	logger.Info("file-only", slog.String("key", "value"))
	require.NoError(t, file.Sync())

	contents := readTextFile(t, path)
	assert.Contains(t, contents, "file-only")
	assert.Contains(t, contents, "value")
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}
