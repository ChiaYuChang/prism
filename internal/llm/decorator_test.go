package llm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ChiaYuChang/prism/internal/llm"
	llmmocks "github.com/ChiaYuChang/prism/internal/llm/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestInstrumentGeneratorRecordsSuccessMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	metrics, err := llm.NewMetrics(meterProvider.Meter("test"))
	require.NoError(t, err)

	base := llmmocks.NewMockGenerator(t)
	req := &llm.GenerateRequest{Model: "configured-model"}
	base.EXPECT().Generate(mock.Anything, req).Return(&llm.GenerateResponse{
		Model: "provider-resolved-model",
		Usage: llm.TokenUsage{
			Input:     10,
			Output:    20,
			Total:     35,
			Cached:    2,
			Tool:      3,
			Reasoning: 4,
			Thought:   5,
		},
	}, nil)

	instrumented := llm.InstrumentGenerator(base, metrics, "llm.test")
	resp, err := instrumented.Generate(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	rm := collectLLMMetrics(t, reader)
	attrs := map[string]string{
		"provider":  "llm.test",
		"model":     "configured-model",
		"operation": "generate",
		"result":    "ok",
	}
	require.Equal(t, int64(1), llmCounterValue(t, rm, "prism.llm.requests", attrs))
	require.Equal(t, uint64(1), llmHistogramCount(t, rm, "prism.llm.request.duration", attrs))

	for tokenType, want := range map[string]int64{
		"input":     10,
		"output":    20,
		"total":     35,
		"cached":    2,
		"tool":      3,
		"reasoning": 4,
		"thought":   5,
	} {
		tokenAttrs := map[string]string{
			"provider":   "llm.test",
			"model":      "configured-model",
			"token.type": tokenType,
		}
		require.Equal(t, want, llmCounterValue(t, rm, "prism.llm.tokens", tokenAttrs))
	}
}

func TestInstrumentGeneratorRecordsFailureMetrics(t *testing.T) {
	reader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	metrics, err := llm.NewMetrics(meterProvider.Meter("test"))
	require.NoError(t, err)

	base := llmmocks.NewMockGenerator(t)
	req := &llm.GenerateRequest{Model: "configured-model"}
	base.EXPECT().Generate(mock.Anything, req).Return(nil, errors.New("provider down"))

	instrumented := llm.InstrumentGenerator(base, metrics, "llm.test")
	_, err = instrumented.Generate(context.Background(), req)
	require.Error(t, err)

	rm := collectLLMMetrics(t, reader)
	attrs := map[string]string{
		"provider":  "llm.test",
		"model":     "configured-model",
		"operation": "generate",
		"result":    "failed",
	}
	require.Equal(t, int64(1), llmCounterValue(t, rm, "prism.llm.requests", attrs))
	require.Equal(t, uint64(1), llmHistogramCount(t, rm, "prism.llm.request.duration", attrs))
	require.Equal(t, int64(0), llmCounterTotal(t, rm, "prism.llm.tokens"))
}

func TestInstrumentEmbedderRecordsMetrics(t *testing.T) {
	tcs := []struct {
		name   string
		result string
		err    error
	}{
		{name: "success", result: "ok"},
		{name: "failure", result: "failed", err: errors.New("provider down")},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			reader := metric.NewManualReader()
			meterProvider := metric.NewMeterProvider(metric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
			metrics, err := llm.NewMetrics(meterProvider.Meter("test"))
			require.NoError(t, err)

			base := llmmocks.NewMockEmbedder(t)
			req := &llm.EmbedRequest{Model: "embed-model", Input: []string{"text"}}
			base.EXPECT().Embed(mock.Anything, req).Return(&llm.EmbedResponse{Model: "provider-embed-model"}, tc.err)

			instrumented := llm.InstrumentEmbedder(base, metrics, "llm.test")
			_, err = instrumented.Embed(context.Background(), req)
			if tc.err != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			rm := collectLLMMetrics(t, reader)
			attrs := map[string]string{
				"provider":  "llm.test",
				"model":     "embed-model",
				"operation": "embed",
				"result":    tc.result,
			}
			require.Equal(t, int64(1), llmCounterValue(t, rm, "prism.llm.requests", attrs))
			require.Equal(t, uint64(1), llmHistogramCount(t, rm, "prism.llm.request.duration", attrs))
			require.Equal(t, int64(0), llmCounterTotal(t, rm, "prism.llm.tokens"))
		})
	}
}

func TestInstrumentProviderWrapsGenerateAndEmbed(t *testing.T) {
	reader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(context.Background())) })
	metrics, err := llm.NewMetrics(meterProvider.Meter("test"))
	require.NoError(t, err)

	base := llmmocks.NewMockProvider(t)
	genReq := &llm.GenerateRequest{Model: "gen-model"}
	embedReq := &llm.EmbedRequest{Model: "embed-model", Input: []string{"text"}}
	base.EXPECT().Generate(mock.Anything, genReq).Return(&llm.GenerateResponse{Usage: llm.TokenUsage{Total: 1}}, nil)
	base.EXPECT().Embed(mock.Anything, embedReq).Return(&llm.EmbedResponse{}, nil)

	instrumented := llm.InstrumentProvider(base, metrics, "llm.test")
	_, err = instrumented.Generate(context.Background(), genReq)
	require.NoError(t, err)
	_, err = instrumented.Embed(context.Background(), embedReq)
	require.NoError(t, err)

	rm := collectLLMMetrics(t, reader)
	require.Equal(t, int64(1), llmCounterValue(t, rm, "prism.llm.requests", map[string]string{"operation": "generate"}))
	require.Equal(t, int64(1), llmCounterValue(t, rm, "prism.llm.requests", map[string]string{"operation": "embed"}))
}

func collectLLMMetrics(t *testing.T, reader *metric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

func llmCounterValue(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string) int64 {
	t.Helper()
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				if attributesMatch(dp.Attributes, attrs) {
					total += dp.Value
				}
			}
		}
	}
	return total
}

func llmCounterTotal(t *testing.T, rm metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	return llmCounterValue(t, rm, name, map[string]string{})
}

func llmHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string) uint64 {
	t.Helper()
	var total uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			for _, dp := range histogram.DataPoints {
				if attributesMatch(dp.Attributes, attrs) {
					total += dp.Count
				}
			}
		}
	}
	return total
}

func attributesMatch(set attribute.Set, attrs map[string]string) bool {
	for key, want := range attrs {
		got, found := set.Value(attribute.Key(key))
		if !found || got.AsString() != want {
			return false
		}
	}
	return true
}
