package infra

import (
	"log/slog"
	"sync/atomic"

	"github.com/go-playground/mold/v4"
	"github.com/go-playground/mold/v4/modifiers"
	"github.com/go-playground/validator/v10"
	"go.opentelemetry.io/otel"
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
	SetValidator(validator.New(validator.WithRequiredStructEnabled()))
	SetTransformer(modifiers.New())
	SetTracer(otel.Tracer("prism"))
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
