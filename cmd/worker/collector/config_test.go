package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig(nil)
	require.NoError(t, err)

	assert.Equal(t, 8093, cfg.HealthPort)
	assert.Equal(t, 30*time.Second, cfg.HTTPTimeout)
	assert.Equal(t, 2*time.Minute, cfg.MaxProcessingTime)
	assert.Equal(t, "localhost", cfg.Postgres.Host)
	assert.Equal(t, "nats", cfg.MessengerType)
	assert.Equal(t, "", cfg.Archive)
	require.NotNil(t, cfg.Messenger)
}

func TestLoadConfigShippedConfig(t *testing.T) {
	cfg, err := LoadConfig([]string{"--config", filepath.Join("..", "..", "..", "configs", "worker", "collector", "config.yaml")})
	require.NoError(t, err)

	assert.Equal(t, 8093, cfg.HealthPort)
	assert.Equal(t, "/app/configs/worker/collector/parsers.yaml", cfg.ParsersConfigPath)
	assert.Equal(t, "file:///app/archives", cfg.Archive)
	assert.Equal(t, "postgres", cfg.Postgres.Host)
	assert.Equal(t, "prism.collector", cfg.Telemetry.ServiceName)
	assert.Equal(t, "/logs/collector.json", cfg.Logger.File.File)
}

func TestLoadConfigFromFlags(t *testing.T) {
	cfg, err := LoadConfig([]string{
		"--health-port=9092",
		"--http-timeout=45s",
		"--max-processing-time=90s",
		"--pg-host=127.0.0.1",
		"--pg-port=5433",
		"--messenger-type=gochannel",
		"--archive=file:///tmp/archives",
	})
	require.NoError(t, err)

	assert.Equal(t, 9092, cfg.HealthPort)
	assert.Equal(t, 45*time.Second, cfg.HTTPTimeout)
	assert.Equal(t, 90*time.Second, cfg.MaxProcessingTime)
	assert.Equal(t, "127.0.0.1", cfg.Postgres.Host)
	assert.Equal(t, 5433, cfg.Postgres.Port)
	assert.Equal(t, "gochannel", cfg.MessengerType)
	assert.Equal(t, "file:///tmp/archives", cfg.Archive)
}

func TestLoadConfigTelemetryFlags(t *testing.T) {
	cfg, err := LoadConfig([]string{
		"--otel-enabled",
		"--otel-service-name=prism.collector.test",
		"--otel-service-version=dev",
		"--otel-environment=test",
		"--otel-endpoint=collector:4317",
		"--otel-sample-ratio=0.25",
		"--otel-timeout=3s",
		"--messenger-type=gochannel",
	})
	require.NoError(t, err)

	assert.True(t, cfg.Telemetry.Enabled)
	assert.Equal(t, "prism.collector.test", cfg.Telemetry.ServiceName)
	assert.Equal(t, "dev", cfg.Telemetry.ServiceVersion)
	assert.Equal(t, "test", cfg.Telemetry.Environment)
	assert.Equal(t, "collector:4317", cfg.Telemetry.Endpoint)
	assert.Equal(t, 0.25, cfg.Telemetry.SampleRatio)
	assert.Equal(t, 3*time.Second, cfg.Telemetry.Timeout)
}

func TestLoadConfigS3FromFlags(t *testing.T) {
	cfg, err := LoadConfig([]string{
		"--archive=s3://mybucket/errors",
		"--s3-endpoint=http://localhost:9000",
		"--s3-region=us-west-2",
		"--s3-access-key=AKIA",
		"--s3-secret-key=SECRET",
		"--s3-use-path-style=false",
	})
	require.NoError(t, err)

	assert.Equal(t, "s3://mybucket/errors", cfg.Archive)
	assert.Equal(t, "http://localhost:9000", cfg.S3.Endpoint)
	assert.Equal(t, "us-west-2", cfg.S3.Region)
	assert.Equal(t, "AKIA", cfg.S3.AccessKey)
	assert.Equal(t, "SECRET", cfg.S3.SecretKey)
	assert.Equal(t, false, cfg.S3.UsePathStyle)
}

func TestLoadConfigFromEnvironment(t *testing.T) {
	require.NoError(t, os.Setenv("PRISM_COLLECTOR_WORKER_MAX_PROCESSING_TIME", "75s"))
	require.NoError(t, os.Setenv("PRISM_COLLECTOR_WORKER_POSTGRES_USERNAME", "tester"))
	defer func() {
		_ = os.Unsetenv("PRISM_COLLECTOR_WORKER_MAX_PROCESSING_TIME")
		_ = os.Unsetenv("PRISM_COLLECTOR_WORKER_POSTGRES_USERNAME")
	}()

	cfg, err := LoadConfig(nil)
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
