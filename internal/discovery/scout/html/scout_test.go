package htmlscout_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	scoutconfig "github.com/ChiaYuChang/prism/internal/discovery/scout/config"
	htmlscout "github.com/ChiaYuChang/prism/internal/discovery/scout/html"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestHTMLScoutDiscover(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configPath string
		rawURL     string
		transport  func(t *testing.T) http.RoundTripper
		items      map[string]struct {
			Title   string
			PubDate string
		}
	}{
		{
			name:       "dpp",
			configPath: filepath.Join("..", "..", "..", "..", "configs", "worker", "discovery", "scouts.yaml"),
			rawURL:     "https://www.dpp.org.tw/media",
			transport: func(t *testing.T) http.RoundTripper {
				t.Helper()

				body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "synthetic", "discovery", "scout", "dpp_00.html"))
				require.NoError(t, err)

				return testutils.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(testutils.NewReader(body)),
						Request:    req,
					}, nil
				})
			},
			items: map[string]struct {
				Title   string
				PubDate string
			}{
				"/media/contents/90001": {Title: "Synthetic DPP Directory Item 1", PubDate: "2026-03-29"},
				"/media/contents/90002": {Title: "Synthetic DPP Directory Item 2", PubDate: "2026-03-28"},
			},
		},
		{
			name:       "tpp",
			configPath: filepath.Join("..", "..", "..", "..", "configs", "worker", "discovery", "scouts.yaml"),
			rawURL:     "https://www.tpp.org.tw/media?page=1",
			transport: func(t *testing.T) http.RoundTripper {
				t.Helper()

				return testutils.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
					rawPage := req.URL.Query().Get("page")
					if rawPage == "" {
						rawPage = "01"
					}

					page, err := strconv.Atoi(rawPage)
					require.NoError(t, err)

					body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "synthetic", "discovery", "scout", fmt.Sprintf("tpp_%02d.html", page)))
					require.NoError(t, err)

					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(testutils.NewReader(body)),
						Request:    req,
					}, nil
				})
			},
			items: map[string]struct {
				Title   string
				PubDate string
			}{
				"/newsdetail/91001": {Title: "Synthetic TPP Directory Item 1", PubDate: "2026-03-29"},
				"/newsdetail/91002": {Title: "Synthetic TPP Directory Item 2", PubDate: "2026-03-28"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := scoutconfig.ReadFile(tt.configPath)
			require.NoError(t, err)

			repo, err := scoutconfig.New(cfg)
			require.NoError(t, err)

			spec, ok := repo.HTML(tt.name)
			require.True(t, ok)

			client := &http.Client{Transport: tt.transport(t)}
			scout, err := htmlscout.New(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), client, spec.Config)
			require.NoError(t, err)

			got, err := scout.Discover(context.Background(), tt.rawURL)
			require.NoError(t, err)
			require.Len(t, got, len(tt.items))

			for _, g := range got {
				u, err := url.Parse(g.URL)
				require.NoError(t, err)

				item, ok := tt.items[u.Path]
				require.True(t, ok, u.Path, "not found")
				require.Equal(t, item.Title, g.Title)
				require.Equal(t, item.PubDate, g.PublishedAt.Format("2006-01-02"))
			}
		})
	}
}
