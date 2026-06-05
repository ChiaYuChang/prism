package obs

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggingConfig_NoOTELHeaderLeak(t *testing.T) {
	const header = "bearer-abcdef-0123456789"
	cfg := LoggingConfig{
		Level: "info",
		OTEL: OTELLogConfig{
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

func TestBuildLoggingHandlers_FileOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prism.log")
	handlers, file, shutdown, err := BuildLoggingHandlers(context.Background(), LoggingConfig{
		Level: "info",
		File:  FileLogConfig{Enable: true, File: path},
	})
	require.NoError(t, err)
	require.NotNil(t, file)
	defer func() { _ = file.Close() }()
	defer func() { _ = shutdown(context.Background()) }()
	logger := prismlogger.NewLoggerFromHandlers(handlers)

	logger.Info("file-only", slog.String("key", "value"))
	require.NoError(t, file.Sync())

	contents := readTextFile(t, path)
	assert.Contains(t, contents, "file-only")
	assert.Contains(t, contents, "value")
}

func TestRegisterBindAndLoadLoggingConfig(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	RegisterLoggingFlags(fs, DefaultLoggingConfig("prism.test"))
	require.NoError(t, fs.Parse([]string{
		"--log-level=debug",
		"--log-path=/tmp/prism.log",
		"--log-console-enable=false",
		"--log-file-level=warn",
		"--log-otel-enable",
		"--log-otel-url=collector:4317",
		"--log-otel-headers=authorization=masked-value",
	}))

	v := viper.New()
	require.NoError(t, BindLoggingFlags(v, fs))
	cfg, err := LoadLoggingConfig(v)
	require.NoError(t, err)

	assert.Equal(t, "debug", cfg.Level)
	assert.False(t, cfg.Console.Enable)
	assert.True(t, cfg.File.Enable)
	assert.Equal(t, "/tmp/prism.log", cfg.File.File)
	assert.Equal(t, "warn", cfg.File.Level)
	assert.True(t, cfg.OTEL.Enable)
	assert.Equal(t, "collector:4317", cfg.OTEL.URL)
	assert.Equal(t, "prism.test", cfg.OTEL.ServiceName)
	assert.Equal(t, "masked-value", cfg.OTEL.Headers["authorization"])
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}
