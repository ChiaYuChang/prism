package llm

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Metrics contains OpenTelemetry instruments for LLM provider operations.
type Metrics struct {
	request *requestMetrics
	tokens  metric.Int64Counter
}

type requestMetrics struct {
	count    metric.Int64Counter
	duration metric.Float64Histogram
}

// NewMetrics creates LLM provider metrics using the supplied meter.
func NewMetrics(meter metric.Meter) (*Metrics, error) {
	requests, err := meter.Int64Counter(
		"prism.llm.requests",
		metric.WithDescription("Count of LLM provider request outcomes."),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create LLM request counter: %w", err)
	}

	requestDuration, err := meter.Float64Histogram(
		"prism.llm.request.duration",
		metric.WithDescription("LLM provider request duration."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create LLM request duration histogram: %w", err)
	}

	tokens, err := meter.Int64Counter(
		"prism.llm.tokens",
		metric.WithDescription("Count of LLM provider tokens by token type."),
		metric.WithUnit("{token}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create LLM token counter: %w", err)
	}

	return &Metrics{
		request: &requestMetrics{
			count:    requests,
			duration: requestDuration,
		},
		tokens: tokens,
	}, nil
}
