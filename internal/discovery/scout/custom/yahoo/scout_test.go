package yahoo_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery/scout/custom/yahoo"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestScoutDiscover(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "testdata", "synthetic", "discovery", "scout", "yahoo_politics.html"))
	require.NoError(t, err)

	client := &http.Client{
		Transport: testutils.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(testutils.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}

	items := map[string]struct {
		Title     string
		PubDate   string
		Publisher string
	}{
		"https://tw.news.yahoo.com/synthetic-politics-001.html": {
			Title:     "Synthetic Yahoo Politics Item 1",
			PubDate:   "2026-03-29",
			Publisher: "Synthetic Publisher A",
		},
		"https://tw.news.yahoo.com/synthetic-politics-002.html": {
			Title:     "Synthetic Yahoo Politics Item 2",
			PubDate:   "2026-03-29",
			Publisher: "Synthetic Publisher B",
		},
	}

	scout, err := yahoo.New(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), client, yahoo.Config{
		Name:     "yahoo",
		Format:   "custom",
		SpanName: "discovery.scout.custom.yahoo.discover",
		Headers:  htmlHeaders(),
	})
	require.NoError(t, err)

	got, err := scout.Discover(context.Background(), "https://tw.news.yahoo.com/politics/")
	require.NoError(t, err)
	require.Len(t, got, 2)

	for _, g := range got {
		item, ok := items[g.URL]
		if !ok {
			continue
		}

		require.Equal(t, item.Title, g.Title)
		require.Equal(t, item.PubDate, g.PublishedAt.Format("2006-01-02"))
		require.Equal(t, item.Publisher, g.Metadata["publisher"])
	}
}

func htmlHeaders() map[string]string {
	return map[string]string{
		"User-Agent":                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
		"Accept-Language":           "zh-TW,zh;q=0.9,en-US;q=0.8,en;q=0.7",
		"Referer":                   "https://www.google.com/",
		"Sec-Ch-Ua":                 `"Chromium";v="136", "Not(A:Brand";v="24", "Google Chrome";v="136"`,
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "cross-site",
		"Upgrade-Insecure-Requests": "1",
	}
}
