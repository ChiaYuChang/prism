package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeS3ConfigFile writes a minimal YAML file that satisfies the S3Config
// required fields. S3 flag bindings do not populate the nested "s3.*"
// mapstructure keys, so tests provide them via --config instead.
func writeS3ConfigFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "collector.yaml")
	contents := `s3:
  endpoint: http://localhost:8333
  bucket: prism-archive
  access-key: any
  secret-key: any
`
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig([]string{"--config=" + writeS3ConfigFile(t)})
	require.NoError(t, err)

	assert.Equal(t, 8082, cfg.HealthPort)
	assert.Equal(t, 30*time.Second, cfg.HTTPTimeout)
	assert.Equal(t, 2*time.Minute, cfg.MaxProcessingTime)
	assert.Equal(t, "localhost", cfg.Postgres.Host)
	assert.Equal(t, "nats", cfg.MessengerType)
	require.NotNil(t, cfg.Messenger)
}

func TestLoadConfigFromFlags(t *testing.T) {
	cfg, err := LoadConfig([]string{
		"--config=" + writeS3ConfigFile(t),
		"--health-port=9092",
		"--http-timeout=45s",
		"--max-processing-time=90s",
		"--pg-host=127.0.0.1",
		"--pg-port=5433",
		"--messenger-type=gochannel",
	})
	require.NoError(t, err)

	assert.Equal(t, 9092, cfg.HealthPort)
	assert.Equal(t, 45*time.Second, cfg.HTTPTimeout)
	assert.Equal(t, 90*time.Second, cfg.MaxProcessingTime)
	assert.Equal(t, "127.0.0.1", cfg.Postgres.Host)
	assert.Equal(t, 5433, cfg.Postgres.Port)
	assert.Equal(t, "gochannel", cfg.MessengerType)
}

func TestLoadConfigFromEnvironment(t *testing.T) {
	require.NoError(t, os.Setenv("PRISM_COLLECTOR_WORKER_MAX_PROCESSING_TIME", "75s"))
	require.NoError(t, os.Setenv("PRISM_COLLECTOR_WORKER_POSTGRES_USERNAME", "tester"))
	defer func() {
		_ = os.Unsetenv("PRISM_COLLECTOR_WORKER_MAX_PROCESSING_TIME")
		_ = os.Unsetenv("PRISM_COLLECTOR_WORKER_POSTGRES_USERNAME")
	}()

	cfg, err := LoadConfig([]string{"--config=" + writeS3ConfigFile(t)})
	require.NoError(t, err)

	assert.Equal(t, 75*time.Second, cfg.MaxProcessingTime)
	assert.Equal(t, "tester", cfg.Postgres.Username)
}

func TestLoadConfigValidationFailed(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "health-port too low", args: []string{"--health-port=1"}},
		{name: "http-timeout too short", args: []string{"--http-timeout=0s"}},
		{name: "max-processing-time too short", args: []string{"--max-processing-time=0s"}},
		{name: "invalid messenger type", args: []string{"--messenger-type=invalid"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadConfig(tt.args)
			assert.Error(t, err)
		})
	}
}
