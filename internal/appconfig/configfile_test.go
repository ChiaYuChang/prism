package appconfig

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadTemplatedConfigStaticYAML(t *testing.T) {
	path := writeConfigFile(t, "config.yaml", "name: prism\n")

	body, configType, err := ReadTemplatedConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "yaml", configType)
	assert.Equal(t, "name: prism\n", string(body))
}

func TestReadTemplatedConfigEnvDefault(t *testing.T) {
	startupLogs.Reset()
	t.Cleanup(startupLogs.Reset)
	path := writeConfigFile(t, "config.yaml", "name: '{{ env \"PRISM_TEST_NAME\" \"fallback\" }}'\n")

	body, _, err := ReadTemplatedConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "name: 'fallback'\n", string(body))
	require.Len(t, startupLogs.msg, 1)
}

func TestReadTemplatedConfigEnvOverride(t *testing.T) {
	t.Setenv("PRISM_TEST_NAME", "override")
	path := writeConfigFile(t, "config.yaml", "name: '{{ env \"PRISM_TEST_NAME\" \"fallback\" }}'\n")

	body, _, err := ReadTemplatedConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "name: 'override'\n", string(body))
}

func TestReadConfigFile(t *testing.T) {
	startupLogs.Reset()
	t.Cleanup(startupLogs.Reset)
	t.Setenv("PRISM_TEST_NAME", "loaded")
	path := writeConfigFile(t, "config.yaml", "name: '{{ env \"PRISM_TEST_NAME\" \"fallback\" }}'\n")
	v := viper.New()

	require.NoError(t, ReadConfigFile(v, path))
	assert.Equal(t, "loaded", v.GetString("name"))
	require.Len(t, startupLogs.msg, 1)
	assert.Equal(t, "loaded config file", startupLogs.msg[0].Message)
}

func TestReadTemplatedConfigUnsupportedExtension(t *testing.T) {
	path := writeConfigFile(t, "config.conf", "name: prism\n")

	_, _, err := ReadTemplatedConfig(path)
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported config file extension")
}

func writeConfigFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))
	return path
}

type recordHandler struct {
	records []slog.Record
}

func (h *recordHandler) Enabled(ctx context.Context, l slog.Level) bool { return true }
func (h *recordHandler) Handle(ctx context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *recordHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *recordHandler) WithGroup(name string) slog.Handler       { return h }

func TestPendingLogQueue(t *testing.T) {
	givenTime := time.Now().Add(-5 * time.Minute)

	var q pendingLogQueue
	q.AddWithTime(givenTime, slog.LevelWarn, "test warning", slog.String("key", "val"))

	require.Len(t, q.msg, 1)
	assert.Equal(t, slog.LevelWarn, q.msg[0].Level)
	assert.Equal(t, "test warning", q.msg[0].Message)
	assert.Equal(t, []slog.Attr{slog.String("key", "val")}, q.msg[0].Attr)
	assert.True(t, q.msg[0].Time.Equal(givenTime))

	// Setup custom logger to verify the flushed record
	rh := &recordHandler{}
	logger := slog.New(rh)
	oldDefault := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(oldDefault)

	// Flush and verify that the handler received the record with the correct custom time
	q.Flush()
	assert.Empty(t, q.msg)

	require.Len(t, rh.records, 1)
	assert.Equal(t, "test warning", rh.records[0].Message)
	assert.Equal(t, slog.LevelWarn, rh.records[0].Level)
	assert.True(t, rh.records[0].Time.Equal(givenTime), "Expected time to be overridden to givenTime")
}

func TestPendingLogQueueTimeSerialization(t *testing.T) {
	var buf bytes.Buffer
	// Create a standard JSON handler writing to buf
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// format time consistently for easier assertion
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.String(slog.TimeKey, a.Value.Time().UTC().Format(time.RFC3339))
			}
			return a
		},
	})

	logger := slog.New(handler)
	oldDefault := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(oldDefault)

	targetTime := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	var q pendingLogQueue
	q.AddWithTime(targetTime, slog.LevelInfo, "hello time", slog.String("env", "test"))
	q.Flush()

	output := buf.String()
	assert.Contains(t, output, `"time":"2026-06-14T12:00:00Z"`)
	assert.Contains(t, output, `"msg":"hello time"`)
	assert.Contains(t, output, `"env":"test"`)
}
