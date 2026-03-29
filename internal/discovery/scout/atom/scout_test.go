package atomscout_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery"
	atomscout "github.com/ChiaYuChang/prism/internal/discovery/scout/atom"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestScoutDiscover(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		rawURL   string
		fixture  string
		scoutCfg atomscout.Config
		expected struct {
			URL     string
			Title   string
			PubDate string
			Count   int
		}
	}{
		{
			name:    "pts",
			rawURL:  "https://news.pts.org.tw/xml/newsfeed.xml",
			fixture: "pts_rss.xml",
			scoutCfg: atomscout.Config{
				Name:     "pts",
				Format:   "atom",
				SpanName: discovery.ScoutDiscoverSpanName("atom", "pts"),
			},
			expected: struct {
				URL     string
				Title   string
				PubDate string
				Count   int
			}{
				URL:     "https://news.pts.org.tw/article/801147",
				Title:   "明汽柴油再調漲 中油已吸收逾69.9億元",
				PubDate: "2026-03-29",
				Count:   25,
			},
		},
		{
			name:    "kmt",
			rawURL:  "https://www.kmt.org.tw/feed",
			fixture: "kmt_01.xml",
			scoutCfg: atomscout.Config{
				Name:     "kmt",
				Format:   "atom",
				SpanName: discovery.ScoutDiscoverSpanName("atom", "kmt"),
			},
			expected: struct {
				URL     string
				Title   string
				PubDate string
				Count   int
			}{
				URL:     "https://www.kmt.org.tw/2025/09/blog-post_10.html",
				Title:   "朱立倫主席：遲來的交保不是正義，應給柯文哲空間證明清白",
				PubDate: "2025-09-10",
				Count:   10,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			body, err := os.ReadFile(filepath.Join("..", "_testdata", c.fixture))
			require.NoError(t, err)

			client := &http.Client{
				Transport: testutils.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
					header := make(http.Header)
					header.Set("Content-Type", "application/atom+xml; charset=UTF-8")
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     header,
						Body:       io.NopCloser(testutils.NewReader(body)),
						Request:    req,
					}, nil
				}),
			}

			scout, err := atomscout.New(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), client, c.scoutCfg)
			require.NoError(t, err)

			got, err := scout.Discover(context.Background(), c.rawURL)
			require.NoError(t, err)
			require.Len(t, got, c.expected.Count)
			require.Equal(t, c.expected.URL, got[0].URL)
			require.Equal(t, c.expected.Title, got[0].Title)
			require.Equal(t, c.expected.PubDate, got[0].PublishedAt.Format("2006-01-02"))
		})
	}
}
