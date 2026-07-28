package main

import (
	"path/filepath"
	"testing"

	app "github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigShippedConfig(t *testing.T) {
	t.Setenv("SEAWEEDFS_S3_REGION", "us-east-1")
	t.Setenv("PRISM_WORKER_OTEL_ENABLED", "true")
	t.Setenv("OTEL_COLLECTOR_ENDPOINT", "otel-collector:4317")

	cfg, err := LoadConfig([]string{"--config", filepath.Join("..", "..", "..", "..", "configs", "worker", "collector", "archive", "config.yaml")})
	require.NoError(t, err)
	natsCfg, ok := cfg.Messenger.(*app.NatsConfig)
	require.True(t, ok)
	require.Equal(t, "prism_archive", natsCfg.Stream)
	require.Equal(t, "archive-worker", natsCfg.Consumer)
	require.NotNil(t, natsCfg.AutoProvision)
	require.False(t, *natsCfg.AutoProvision)
}
