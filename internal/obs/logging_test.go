package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChiaYuChang/prism/pkg/units"
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

func TestNewLoggerFromHandlersInjectsTraceID(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerFromHandlers([]slog.Handler{slog.NewJSONHandler(&buf, nil)})

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	logger.InfoContext(WithTraceID(context.Background(), traceID), "with trace")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	assert.Equal(t, traceID, record["trace_id"])
}

func TestBuildLoggingHandlers_FileOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prism.log")
	handlers, file, shutdown, err := BuildLoggingHandlers(context.Background(), LoggingConfig{
		Level: "info",
		File:  FileLogConfig{Enable: true, File: path, MaxSize: units.Bytes("10MiB"), MaxFiles: 3},
	})
	require.NoError(t, err)
	require.NotNil(t, file)
	defer func() { _ = file.Close() }()
	defer func() { _ = shutdown(context.Background()) }()
	logger := NewLoggerFromHandlers(handlers)

	logger.Info("file-only", slog.String("key", "value"))
	require.NoError(t, file.Sync())

	contents := readTextFile(t, path+".0")
	assert.Contains(t, contents, "file-only")
	assert.Contains(t, contents, "value")
}

func TestBuildLoggingHandlers_FileUsesJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prism.log")
	handlers, file, shutdown, err := BuildLoggingHandlers(context.Background(), LoggingConfig{
		Level: "info",
		File:  FileLogConfig{Enable: true, File: path, MaxSize: units.Bytes("10MiB"), MaxFiles: 3},
	})
	require.NoError(t, err)
	require.NotNil(t, file)
	defer func() { _ = file.Close() }()
	defer func() { _ = shutdown(context.Background()) }()
	logger := NewLoggerFromHandlers(handlers)

	logger.Info("json-file", slog.String("key", "value"))
	require.NoError(t, file.Sync())

	lines := strings.Split(strings.TrimSpace(readTextFile(t, path+".0")), "\n")
	var record map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &record))
	assert.Equal(t, "json-file", record["msg"])
	assert.Equal(t, "value", record["key"])
}

func TestBuildLoggingHandlers_RotatesSlot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prism.log")

	handlers, file, shutdown, err := BuildLoggingHandlers(context.Background(), LoggingConfig{
		Level: "info",
		File:  FileLogConfig{Enable: true, File: path, MaxSize: units.Bytes("50B"), MaxFiles: 3},
	})
	require.NoError(t, err)
	require.NotNil(t, file)
	defer func() { _ = file.Close() }()
	defer func() { _ = shutdown(context.Background()) }()

	logger := NewLoggerFromHandlers(handlers)
	logger.Info("some logs that will exceed fifty bytes limit")
	logger.Info("more logs to ensure rotation happens")
	require.NoError(t, file.Sync())

	content0 := readTextFile(t, path+".0")
	content1 := readTextFile(t, path+".1")

	assert.NotEmpty(t, content0)
	assert.NotEmpty(t, content1)
}

func TestBuildLoggingHandlers_ConsoleAndFileLevelsCanDiffer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prism.log")
	handlers, file, shutdown, err := BuildLoggingHandlers(context.Background(), LoggingConfig{
		Level:   "info",
		Console: ConsoleLogConfig{Enable: true, Level: "debug"},
		File:    FileLogConfig{Enable: true, File: path, Level: "warn"},
	})
	require.NoError(t, err)
	require.NotNil(t, file)
	defer func() { _ = file.Close() }()
	defer func() { _ = shutdown(context.Background()) }()
	require.Len(t, handlers, 2)

	ctx := context.Background()
	assert.True(t, handlers[0].Enabled(ctx, slog.LevelDebug), "console should honor debug override")
	assert.False(t, handlers[1].Enabled(ctx, slog.LevelInfo), "file should suppress info below warn override")
	assert.True(t, handlers[1].Enabled(ctx, slog.LevelWarn), "file should allow warn override")
}

func TestRegisterBindAndLoadLoggingConfig(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	RegisterLoggingFlags(fs, DefaultLoggingConfig("prism.test"))
	require.NoError(t, fs.Parse([]string{
		"--log-level=debug",
		"--log-path=/tmp/prism.log",
		"--log-console-enable=false",
		"--log-file-level=warn",
		"--log-file-max-size=2KiB",
		"--log-file-max-files=3",
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
	gotMaxSize, err := cfg.File.MaxSize.Int64()
	require.NoError(t, err)
	assert.EqualValues(t, 2048, gotMaxSize)
	assert.Equal(t, 3, cfg.File.MaxFiles)
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
