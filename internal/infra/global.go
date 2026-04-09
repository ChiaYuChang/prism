package infra

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/go-playground/mold/v4"
	"github.com/go-playground/mold/v4/modifiers"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var (
	// Using atomic pointers to provide lock-free concurrent access,
	// aligned with the singleton pattern used in the Go standard library.
	defaultLogger      atomic.Pointer[slog.Logger]
	defaultValidator   atomic.Pointer[validator.Validate]
	defaultTransformer atomic.Pointer[mold.Transformer]
	defaultTracer      atomic.Value
)

func init() {
	// Initialize with default instances.
	SetLogger(slog.Default())
	SetValidator(newDefaultValidator())
	SetTransformer(newDefaultTransformer())
	_ = InitAndSetTracer("prism")
}

// Logger returns the project-wide default logger.
func Logger() *slog.Logger {
	return defaultLogger.Load()
}

// SetLogger replaces the project-wide default logger.
func SetLogger(l *slog.Logger) {
	if l != nil {
		defaultLogger.Store(l)
	}
}

// newDefaultValidator creates a new validator with the project-wide default settings.
func newDefaultValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	if err := v.RegisterValidation("uuid7", func(fl validator.FieldLevel) bool {
		uid, err := uuid.Parse(fl.Field().String())
		if err != nil {
			return false
		}
		return uid.Version() == 7
	}); err != nil {
		panic(err)
	}
	return v
}

// Validator returns the project-wide default validator.
func Validator() *validator.Validate {
	return defaultValidator.Load()
}

// SetValidator replaces the project-wide default validator.
func SetValidator(v *validator.Validate) {
	if v != nil {
		defaultValidator.Store(v)
	}
}

// newDefaultTransformer creates a new transformer with the project-wide default settings.
func newDefaultTransformer() *mold.Transformer {
	return modifiers.New()
}

// Transformer returns the project-wide default data transformer (scrubber).
func Transformer() *mold.Transformer {
	return defaultTransformer.Load()
}

// SetTransformer replaces the project-wide default data transformer.
func SetTransformer(t *mold.Transformer) {
	if t != nil {
		defaultTransformer.Store(t)
	}
}

// Tracer returns the project-wide default tracer.
func Tracer() trace.Tracer {
	if t, ok := defaultTracer.Load().(trace.Tracer); ok {
		return t
	}
	return otel.Tracer("prism")
}

// SetTracer replaces the project-wide default tracer.
func SetTracer(t trace.Tracer) {
	if t != nil {
		defaultTracer.Store(t)
	}
}

// InitAndSetTracer initializes a real OpenTelemetry SDK tracer provider and stores
// the named tracer as the project-wide default tracer.
func InitAndSetTracer(name string) func(context.Context) error {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	SetTracer(tp.Tracer(name))
	return tp.Shutdown
}
