package obs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitTelemetry_Disabled(t *testing.T) {
	tel, err := InitTelemetry(context.Background(), TelemetryConfig{})
	require.NoError(t, err)
	require.NotNil(t, tel)
	assert.NotNil(t, tel.Tracer("test"))
	assert.NotNil(t, tel.Meter("test"))
	assert.NoError(t, tel.Shutdown(context.Background()))
}

func TestInitTracing_Disabled(t *testing.T) {
	tracing, err := InitTracing(context.Background(), TelemetryConfig{})
	require.NoError(t, err)
	assert.NotNil(t, tracing.Tracer("test"))
	assert.NoError(t, tracing.Shutdown(context.Background()))
}

func TestInitMetrics_Disabled(t *testing.T) {
	metrics, err := InitMetrics(context.Background(), TelemetryConfig{})
	require.NoError(t, err)
	assert.NotNil(t, metrics.Meter("test"))
	assert.NoError(t, metrics.Shutdown(context.Background()))
}

func TestInitTelemetry_EnabledRequiresEndpoint(t *testing.T) {
	tel, err := InitTelemetry(context.Background(), TelemetryConfig{Enabled: true})
	require.Error(t, err)
	assert.Nil(t, tel)
	assert.Contains(t, err.Error(), "endpoint is empty")
}

func TestInitTelemetry_AcceptsSampleRatios(t *testing.T) {
	for _, ratio := range []float64{0, 0.5, 1} {
		t.Run("ratio", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			tel, err := InitTelemetry(ctx, TelemetryConfig{
				Enabled:     true,
				ServiceName: "prism.test",
				Endpoint:    "localhost:4317",
				Insecure:    true,
				SampleRatio: ratio,
				Timeout:     10 * time.Millisecond,
			})
			require.NoError(t, err)
			require.NotNil(t, tel)
			_ = tel.Shutdown(ctx)
		})
	}
}
