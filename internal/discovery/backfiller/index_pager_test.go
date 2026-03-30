package backfiller_test

import (
	"context"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery/backfiller"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestIndexPagerOffsetPath(t *testing.T) {
	pager, err := backfiller.NewIndexPager(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), backfiller.IndexPagerConfig{
		URLTemplate: "https://www.dpp.org.tw/media/{{.Value}}",
		First:       0,
		Step:        10,
		Mode:        backfiller.PageModeIndex,
	})
	require.NoError(t, err)

	first, err := pager.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "https://www.dpp.org.tw/media/0", first)

	second, err := pager.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "https://www.dpp.org.tw/media/10", second)
}

func TestIndexPagerPageQuery(t *testing.T) {
	pager, err := backfiller.NewIndexPager(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), backfiller.IndexPagerConfig{
		URLTemplate: "https://www.tpp.org.tw/media",
		First:       1,
		Step:        1,
		Mode:        backfiller.PageModeIndex,
		Params: map[string]string{
			"page": "{{.Value}}",
		},
	})
	require.NoError(t, err)

	first, err := pager.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "https://www.tpp.org.tw/media?page=1", first)

	second, err := pager.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "https://www.tpp.org.tw/media?page=2", second)
}

func TestIndexPagerStructuredParams(t *testing.T) {
	pager, err := backfiller.NewIndexPager(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), backfiller.IndexPagerConfig{
		URLTemplate: "https://api.example.com/v1/news/{{.Value}}",
		First:       1,
		Step:        1,
		Params: map[string]string{
			"limit":  "20",
			"offset": "{{mul .Value 20}}",
			"q":      "politics",
		},
	})
	require.NoError(t, err)

	first, err := pager.Next(context.Background())
	require.NoError(t, err)
	require.Contains(t, first, "https://api.example.com/v1/news/1")
	require.Contains(t, first, "limit=20")
	require.Contains(t, first, "offset=20")
	require.Contains(t, first, "q=politics")

	second, err := pager.Next(context.Background())
	require.NoError(t, err)
	require.Contains(t, second, "https://api.example.com/v1/news/2")
	require.Contains(t, second, "offset=40")
}

func TestIndexPagerOmitFirstStructured(t *testing.T) {
	pager, err := backfiller.NewIndexPager(
		testutils.Logger(),
		noop.NewTracerProvider().Tracer("test"),
		backfiller.IndexPagerConfig{
			URLTemplate: "https://example.com/news",
			First:       1,
			Step:        1,
			Params: map[string]string{
				"p": "{{.Value}}",
			},
			OmitFirst: true,
		})
	require.NoError(t, err)

	first, err := pager.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "https://example.com/news", first)

	second, err := pager.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "https://example.com/news?p=2", second)
}
