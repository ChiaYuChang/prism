package obs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"

	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Tracing owns Prism's OpenTelemetry tracer provider.
// Callers that initialize tracing directly should defer Shutdown.
type Tracing struct {
	provider trace.TracerProvider
	shutdown func(context.Context) error
}

// Metrics owns Prism's OpenTelemetry meter provider.
// Callers that initialize metrics directly should defer Shutdown.
type Metrics struct {
	provider metric.MeterProvider
	shutdown func(context.Context) error
}

// Telemetry owns Prism's OpenTelemetry tracing and metrics providers.
// Callers should defer Shutdown after successful initialization.
type Telemetry struct {
	Tracing Tracing
	Metrics Metrics
}

// Tracer returns a tracer from the initialized tracing provider.
func (t Tracing) Tracer(name string) trace.Tracer {
	if t.provider == nil {
		return otel.Tracer(name)
	}
	return t.provider.Tracer(name)
}

// Shutdown flushes and stops the tracing provider.
func (t Tracing) Shutdown(ctx context.Context) error {
	if t.shutdown == nil {
		return nil
	}
	return t.shutdown(ctx)
}

// Meter returns a meter from the initialized metrics provider.
func (m Metrics) Meter(name string) metric.Meter {
	if m.provider == nil {
		return otel.Meter(name)
	}
	return m.provider.Meter(name)
}

// Shutdown flushes and stops the metrics provider.
func (m Metrics) Shutdown(ctx context.Context) error {
	if m.shutdown == nil {
		return nil
	}
	return m.shutdown(ctx)
}

// Tracer returns a tracer from the initialized telemetry provider.
func (t *Telemetry) Tracer(name string) trace.Tracer {
	if t == nil {
		return otel.Tracer(name)
	}
	return t.Tracing.Tracer(name)
}

// Meter returns a meter from the initialized telemetry provider.
func (t *Telemetry) Meter(name string) metric.Meter {
	if t == nil {
		return otel.Meter(name)
	}
	return t.Metrics.Meter(name)
}

// Shutdown flushes and stops telemetry providers.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	return errors.Join(t.Metrics.Shutdown(ctx), t.Tracing.Shutdown(ctx))
}

// InitTelemetry configures shared OTLP trace and metric export. Disabled
// telemetry returns no-op providers and never attempts to connect to OTLP.
func InitTelemetry(ctx context.Context, cfg TelemetryConfig) (*Telemetry, error) {
	tracing, err := InitTracing(ctx, cfg)
	if err != nil {
		return nil, err
	}
	metrics, err := InitMetrics(ctx, cfg)
	if err != nil {
		return nil, errors.Join(err, tracing.Shutdown(ctx))
	}
	return &Telemetry{Tracing: tracing, Metrics: metrics}, nil
}

// InitTracing configures shared OTLP trace export. Disabled telemetry returns a
// no-op provider and never attempts to connect to OTLP.
func InitTracing(ctx context.Context, cfg TelemetryConfig) (Tracing, error) {
	if !cfg.Enabled {
		tp := tracenoop.NewTracerProvider()
		otel.SetTracerProvider(tp)
		return Tracing{
			provider: tp,
			shutdown: func(context.Context) error { return nil },
		}, nil
	}
	if cfg.Endpoint == "" {
		return Tracing{}, fmt.Errorf("otel tracing enabled but endpoint is empty")
	}

	res, err := telemetryResource(ctx, cfg)
	if err != nil {
		return Tracing{}, err
	}

	exp, err := newOTLPTraceExporter(ctx, cfg)
	if err != nil {
		return Tracing{}, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRatio)),
		sdktrace.WithBatcher(exp),
	)
	otel.SetTracerProvider(provider)

	return Tracing{provider: provider, shutdown: provider.Shutdown}, nil
}

// InitMetrics configures shared OTLP metric export. Disabled telemetry returns
// a no-op provider and never attempts to connect to OTLP.
func InitMetrics(ctx context.Context, cfg TelemetryConfig) (Metrics, error) {
	if !cfg.Enabled {
		mp := metricnoop.NewMeterProvider()
		otel.SetMeterProvider(mp)
		return Metrics{
			provider: mp,
			shutdown: func(context.Context) error { return nil },
		}, nil
	}
	if cfg.Endpoint == "" {
		return Metrics{}, fmt.Errorf("otel metrics enabled but endpoint is empty")
	}

	res, err := telemetryResource(ctx, cfg)
	if err != nil {
		return Metrics{}, err
	}

	exp, err := newOTLPMetricExporter(ctx, cfg)
	if err != nil {
		return Metrics{}, err
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
	)
	otel.SetMeterProvider(provider)

	return Metrics{provider: provider, shutdown: provider.Shutdown}, nil
}

func telemetryResource(ctx context.Context, cfg TelemetryConfig) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("deployment.environment", cfg.Environment),
	}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, attribute.String("service.version", cfg.ServiceVersion))
	}
	res, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return nil, fmt.Errorf("create otel telemetry resource: %w", err)
	}
	return res, nil
}

func newOTLPTraceExporter(ctx context.Context, cfg TelemetryConfig) (sdktrace.SpanExporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithTimeout(cfg.Timeout),
	}
	if cfg.Insecure {
		slog.Default().Warn("OTLP trace export uses insecure transport; payloads and headers may travel as plaintext")
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
	}
	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create otel trace exporter: %w", err)
	}
	return exporter, nil
}

func newOTLPMetricExporter(ctx context.Context, cfg TelemetryConfig) (sdkmetric.Exporter, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
		otlpmetricgrpc.WithTimeout(cfg.Timeout),
	}
	if cfg.Insecure {
		slog.Default().Warn("OTLP metric export uses insecure transport; payloads and headers may travel as plaintext")
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlpmetricgrpc.WithHeaders(cfg.Headers))
	}
	exporter, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create otel metric exporter: %w", err)
	}
	return exporter, nil
}
