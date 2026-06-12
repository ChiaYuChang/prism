package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// HTTPMetricsRecorder contains OpenTelemetry instruments for inbound HTTP requests.
type HTTPMetricsRecorder struct {
	requests    metric.Int64Counter
	duration    metric.Float64Histogram
	responseLen metric.Int64Histogram
}

// NewHTTPMetrics creates inbound HTTP request metrics using the supplied meter.
func NewHTTPMetrics(meter metric.Meter) (*HTTPMetricsRecorder, error) {
	requests, err := meter.Int64Counter(
		"prism.http.server.requests",
		metric.WithDescription("Count of inbound HTTP server request outcomes."),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create HTTP server request counter: %w", err)
	}

	duration, err := meter.Float64Histogram(
		"prism.http.server.request.duration",
		metric.WithDescription("Inbound HTTP server request duration."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create HTTP server request duration histogram: %w", err)
	}

	responseLen, err := meter.Int64Histogram(
		"prism.http.server.response.size",
		metric.WithDescription("Inbound HTTP server response body size."),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, fmt.Errorf("create HTTP server response size histogram: %w", err)
	}

	return &HTTPMetricsRecorder{
		requests:    requests,
		duration:    duration,
		responseLen: responseLen,
	}, nil
}

// HTTPMetrics records one metric set for each completed request.
func HTTPMetrics(metrics *HTTPMetricsRecorder) Middleware {
	if metrics == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			defer func() {
				status := rec.statusCode()
				attrs := metric.WithAttributes(
					attribute.String("method", normalizeHTTPMethod(r.Method)),
					attribute.String("route", routePattern(r)),
					attribute.String("status_code", strconv.Itoa(status)),
					attribute.String("result", statusResult(status)),
				)
				metrics.requests.Add(r.Context(), 1, attrs)
				metrics.duration.Record(r.Context(), time.Since(start).Seconds(), attrs)
				metrics.responseLen.Record(r.Context(), int64(rec.bytes), attrs)
			}()

			next.ServeHTTP(rec, r)
		})
	}
}

func normalizeHTTPMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect,
		http.MethodOptions, http.MethodTrace:
		return method
	default:
		return "OTHER"
	}
}

func routePattern(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return "unknown"
}

func statusResult(status int) string {
	switch {
	case status >= 100 && status < 200:
		return "informational"
	case status >= 200 && status < 300:
		return "success"
	case status >= 300 && status < 400:
		return "redirect"
	case status >= 400 && status < 500:
		return "client_error"
	case status >= 500 && status < 600:
		return "server_error"
	default:
		return "unknown"
	}
}
