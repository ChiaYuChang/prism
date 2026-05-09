package parser_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/mocks"
	"github.com/ChiaYuChang/prism/internal/collector/parser"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRegistry_HostMatch(t *testing.T) {
	p := mocks.NewMockParser(t)
	r, err := parser.NewRegistry(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), map[string]collector.Parser{
		"example.com": p,
	}, nil)
	require.NoError(t, err)

	want := &collector.Article{Title: "ok"}
	p.On("Parse", mock.Anything, "https://example.com/x", "data").Return(want, nil)

	got, err := r.Parse(context.Background(), "https://example.com/x", "data")
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestRegistry_HostLowercased(t *testing.T) {
	p := mocks.NewMockParser(t)
	r, err := parser.NewRegistry(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), map[string]collector.Parser{
		"EXAMPLE.com": p,
	}, nil)
	require.NoError(t, err)

	p.On("Parse", mock.Anything, "https://Example.COM/x", "data").Return(&collector.Article{}, nil)

	_, err = r.Parse(context.Background(), "https://Example.COM/x", "data")
	require.NoError(t, err)
}

func TestRegistry_NoMatch_ReturnsErrNoMatchingParser(t *testing.T) {
	r, err := parser.NewRegistry(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), map[string]collector.Parser{
		"example.com": mocks.NewMockParser(t),
	}, nil)
	require.NoError(t, err)

	_, err = r.Parse(context.Background(), "https://other.org/x", "data")
	require.ErrorIs(t, err, parser.ErrNoMatchingParser)
}

func TestRegistry_HostMiss_FallbackUsed(t *testing.T) {
	fallback := mocks.NewMockParser(t)
	r, err := parser.NewRegistry(testutils.Logger(), noop.NewTracerProvider().Tracer("test"),
		map[string]collector.Parser{"example.com": mocks.NewMockParser(t)},
		fallback)
	require.NoError(t, err)

	want := &collector.Article{Title: "fallback ok"}
	fallback.On("Parse", mock.Anything, "https://other.org/x", "data").Return(want, nil)

	got, err := r.Parse(context.Background(), "https://other.org/x", "data")
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestRegistry_HostMatch_FallbackNotInvoked(t *testing.T) {
	host := mocks.NewMockParser(t)
	fallback := mocks.NewMockParser(t)
	r, err := parser.NewRegistry(testutils.Logger(), noop.NewTracerProvider().Tracer("test"),
		map[string]collector.Parser{"example.com": host},
		fallback)
	require.NoError(t, err)

	host.On("Parse", mock.Anything, "https://example.com/x", "data").Return(&collector.Article{}, nil)
	// fallback.On(...) intentionally absent — invocation would fail the test via mock expectations.

	_, err = r.Parse(context.Background(), "https://example.com/x", "data")
	require.NoError(t, err)
}

func TestRegistry_InvalidURL(t *testing.T) {
	r, err := parser.NewRegistry(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), nil, nil)
	require.NoError(t, err)

	_, err = r.Parse(context.Background(), "://bad-url", "data")
	require.Error(t, err)
	require.False(t, errors.Is(err, parser.ErrNoMatchingParser))
}

func TestNewRegistry_NilLogger(t *testing.T) {
	_, err := parser.NewRegistry(nil, noop.NewTracerProvider().Tracer("test"), nil, nil)
	require.ErrorIs(t, err, parser.ErrParamMissing)
}

func TestNewRegistry_NilTracer(t *testing.T) {
	_, err := parser.NewRegistry(testutils.Logger(), nil, nil, nil)
	require.ErrorIs(t, err, parser.ErrParamMissing)
}
