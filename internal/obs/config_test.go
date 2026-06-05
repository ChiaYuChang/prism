package obs

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_NoSecretLeak(t *testing.T) {
	const headerValue = "bearer-abcdef-0123456789"
	cfg := Config{
		Enabled:     true,
		ServiceName: "prism.scheduler",
		Endpoint:    "otel-collector:4317",
		Headers: map[string]string{
			"authorization": headerValue,
		},
		Timeout: 10 * time.Second,
	}

	for _, verb := range []string{"%v", "%+v", "%s"} {
		out := fmt.Sprintf(verb, cfg)
		assert.NotContains(t, out, headerValue, "verb %q leaked OTLP header", verb)
	}

	var buf strings.Builder
	h := slog.NewTextHandler(&buf, nil)
	slog.New(h).Info("otel", slog.Any("config", cfg))
	logged := buf.String()
	assert.NotContains(t, logged, headerValue, "slog.Any leaked OTLP header: %s", logged)
}

func TestRegisterBindAndLoadConfig(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	RegisterTelemetryFlags(fs, DefaultTelemetryConfig("prism.test"))
	require.NoError(t, fs.Parse([]string{
		"--otel-enabled",
		"--otel-service-version=dev",
		"--otel-environment=test",
		"--otel-endpoint=collector:4317",
		"--otel-sample-ratio=0.25",
		"--otel-headers=authorization=masked-value",
		"--otel-timeout=3s",
	}))

	v := viper.New()
	require.NoError(t, BindTelemetryFlags(v, fs))

	cfg, err := LoadTelemetryConfig(v)
	require.NoError(t, err)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, "prism.test", cfg.ServiceName)
	assert.Equal(t, "dev", cfg.ServiceVersion)
	assert.Equal(t, "test", cfg.Environment)
	assert.Equal(t, "collector:4317", cfg.Endpoint)
	assert.Equal(t, 0.25, cfg.SampleRatio)
	assert.Equal(t, "masked-value", cfg.Headers["authorization"])
	assert.Equal(t, 3*time.Second, cfg.Timeout)
}

func TestSaveTelemetryConfig(t *testing.T) {
	v := viper.New()
	want := TelemetryConfig{
		Enabled:        true,
		ServiceName:    "prism.worker.collector",
		ServiceVersion: "dev",
		Environment:    "test",
		Endpoint:       "collector:4317",
		Insecure:       true,
		SampleRatio:    0.5,
		Headers: map[string]string{
			"authorization": "masked-value",
		},
		HeadersFile: "/run/secrets/otel-headers",
		Timeout:     5 * time.Second,
	}

	SaveTelemetryConfig(v, want)
	got, err := LoadTelemetryConfig(v)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
