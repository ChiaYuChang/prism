package scout_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery"
	root "github.com/ChiaYuChang/prism/internal/discovery/scout"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

type stubScout struct{}

func (stubScout) Discover(context.Context, string) ([]model.Candidates, error) {
	return []model.Candidates{{Title: "ok"}}, nil
}

func TestRegistryDiscover(t *testing.T) {
	registry, err := root.NewRegistry(testLogger(), noop.NewTracerProvider().Tracer("test"), map[string]discovery.Scout{
		"www.example.com": stubScout{},
	})
	require.NoError(t, err)

	got, err := registry.Discover(context.Background(), "https://www.example.com/news")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "ok", got[0].Title)
}

func TestRegistryDiscover_NoMatch(t *testing.T) {
	registry, err := root.NewRegistry(testLogger(), noop.NewTracerProvider().Tracer("test"), nil)
	require.NoError(t, err)

	_, err = registry.Discover(context.Background(), "https://www.example.com/news")
	require.Error(t, err)
	require.True(t, errors.Is(err, root.ErrNoMatchingScout))
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
