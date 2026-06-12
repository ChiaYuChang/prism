package llm

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	operationGenerate = "generate"
	operationEmbed    = "embed"
	resultOK          = "ok"
	resultFailed      = "failed"
)

type instrumentedGenerator struct {
	base     Generator
	metrics  *Metrics
	provider string
}

type instrumentedEmbedder struct {
	base     Embedder
	metrics  *Metrics
	provider string
}

type instrumentedProvider struct {
	Generator
	Embedder
}

// InstrumentGenerator wraps base with OpenTelemetry metrics for Generate calls.
func InstrumentGenerator(base Generator, metrics *Metrics, provider string) Generator {
	if base == nil || metrics == nil {
		return base
	}
	return &instrumentedGenerator{base: base, metrics: metrics, provider: provider}
}

// InstrumentEmbedder wraps base with OpenTelemetry metrics for Embed calls.
func InstrumentEmbedder(base Embedder, metrics *Metrics, provider string) Embedder {
	if base == nil || metrics == nil {
		return base
	}
	return &instrumentedEmbedder{base: base, metrics: metrics, provider: provider}
}

// InstrumentProvider wraps both Generate and Embed calls with OpenTelemetry metrics.
func InstrumentProvider(base Provider, metrics *Metrics, provider string) Provider {
	if base == nil || metrics == nil {
		return base
	}
	return &instrumentedProvider{
		Generator: InstrumentGenerator(base, metrics, provider),
		Embedder:  InstrumentEmbedder(base, metrics, provider),
	}
}

func (g *instrumentedGenerator) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	started := time.Now()
	resp, err := g.base.Generate(ctx, req)
	model := requestModel(req)
	result := resultOK
	if err != nil {
		result = resultFailed
	}
	g.metrics.recordRequest(ctx, g.provider, model, operationGenerate, result, started)
	if err == nil && resp != nil {
		g.metrics.recordTokens(ctx, g.provider, model, resp.Usage)
	}
	return resp, err
}

func (e *instrumentedEmbedder) Embed(ctx context.Context, req *EmbedRequest) (*EmbedResponse, error) {
	started := time.Now()
	resp, err := e.base.Embed(ctx, req)
	result := resultOK
	if err != nil {
		result = resultFailed
	}
	e.metrics.recordRequest(ctx, e.provider, embedRequestModel(req), operationEmbed, result, started)
	return resp, err
}

func (m *Metrics) recordRequest(ctx context.Context, provider, model, operation, result string, started time.Time) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("provider", normalizeLLMLabel(provider)),
		attribute.String("model", normalizeLLMLabel(model)),
		attribute.String("operation", operation),
		attribute.String("result", result),
	)
	m.request.count.Add(ctx, 1, attrs)
	m.request.duration.Record(ctx, time.Since(started).Seconds(), attrs)
}

func (m *Metrics) recordTokens(ctx context.Context, provider, model string, usage TokenUsage) {
	if m == nil {
		return
	}
	for _, entry := range []struct {
		kind  string
		count int
	}{
		{kind: "input", count: usage.Input},
		{kind: "output", count: usage.Output},
		{kind: "total", count: usage.Total},
		{kind: "cached", count: usage.Cached},
		{kind: "tool", count: usage.Tool},
		{kind: "reasoning", count: usage.Reasoning},
		{kind: "thought", count: usage.Thought},
	} {
		if entry.count <= 0 {
			continue
		}
		m.tokens.Add(ctx, int64(entry.count), metric.WithAttributes(
			attribute.String("provider", normalizeLLMLabel(provider)),
			attribute.String("model", normalizeLLMLabel(model)),
			attribute.String("token.type", entry.kind),
		))
	}
}

func requestModel(req *GenerateRequest) string {
	if req == nil {
		return "unknown"
	}
	return req.Model
}

func embedRequestModel(req *EmbedRequest) string {
	if req == nil {
		return "unknown"
	}
	return req.Model
}

func normalizeLLMLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
