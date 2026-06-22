package rssscout_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery"
	rootscout "github.com/ChiaYuChang/prism/internal/discovery/scout"
	rssscout "github.com/ChiaYuChang/prism/internal/discovery/scout/rss"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestScoutDiscover(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rawURL   string
		fixture  string
		scoutCfg rssscout.Config
		items    map[string]struct {
			Title   string
			PubDate string
		}
	}{
		{
			name:    "cna",
			rawURL:  "https://feeds.feedburner.com/rsscna/politics",
			fixture: "cna_rss.xml",
			scoutCfg: rssscout.Config{
				Name:     "cna",
				Format:   "rss",
				SpanName: discovery.ScoutDiscoverSpanName("rss", "cna"),
			},
			items: map[string]struct {
				Title   string
				PubDate string
			}{
				"https://www.cna.com.tw/news/synthetic/001.aspx": {Title: "Synthetic CNA RSS Item 1", PubDate: "2026-03-29"},
				"https://www.cna.com.tw/news/synthetic/002.aspx": {Title: "Synthetic CNA RSS Item 2", PubDate: "2026-03-28"},
			},
		},
		{
			name:    "ttv",
			rawURL:  "https://news.ttv.com.tw/rss/politics.xml",
			fixture: "ttv_rss.xml",
			scoutCfg: rssscout.Config{
				Name:     "ttv",
				Format:   "rss",
				SpanName: discovery.ScoutDiscoverSpanName("rss", "ttv"),
			},
			items: map[string]struct {
				Title   string
				PubDate string
			}{
				"https://news.ttv.com.tw/news/synthetic001": {Title: "Synthetic TTV RSS Item 1", PubDate: "2026-03-29"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "synthetic", "discovery", "scout", tt.fixture))
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

			scout, err := rssscout.New(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), client, tt.scoutCfg)
			require.NoError(t, err)

			got, err := scout.Discover(context.Background(), tt.rawURL)
			require.NoError(t, err)

			for _, g := range got {
				item, ok := tt.items[g.URL]
				if !ok {
					continue
				}
				require.Equal(t, rootscout.NormalizeText(item.Title), g.Title)
				require.Equal(t, item.PubDate, g.PublishedAt.Format("2006-01-02"))
			}
		})
	}
}
