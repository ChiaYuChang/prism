package config_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	scoutconfig "github.com/ChiaYuChang/prism/internal/discovery/scout/config"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestBuildRegistry(t *testing.T) {
	cfg, err := scoutconfig.ReadFile(filepath.Join("..", "..", "..", "..", "configs", "worker", "discovery", "scouts.yaml"))
	require.NoError(t, err)

	repo, err := scoutconfig.New(cfg)
	require.NoError(t, err)

	client := &http.Client{
		Transport: testutils.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, contentType, err := fixtureForRequest(req)
			require.NoError(t, err)
			header := make(http.Header)
			if contentType != "" {
				header.Set("Content-Type", contentType)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       io.NopCloser(testutils.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}

	registry, err := scoutconfig.BuildRegistry(repo, testutils.Logger(), noop.NewTracerProvider().Tracer("test"), client)
	require.NoError(t, err)

	tests := []struct {
		url   string
		title string
		date  string
	}{
		{
			url:   "https://www.dpp.org.tw/media",
			title: "Synthetic DPP Directory Item 1",
			date:  "2026-03-29",
		},
		{
			url:   "https://www.tpp.org.tw/media?page=1",
			title: "Synthetic TPP Directory Item 1",
			date:  "2026-03-29",
		},
		{
			url:   "https://feeds.feedburner.com/rsscna/politics",
			title: "Synthetic CNA RSS Item 1",
			date:  "2026-03-29",
		},
		{
			url:   "https://news.pts.org.tw/xml/newsfeed.xml",
			title: "Synthetic PTS Atom Item 1",
			date:  "2026-03-29",
		},
		{
			url:   "https://www.kmt.org.tw/feed",
			title: "Synthetic KMT Atom Item 1",
			date:  "2026-03-29",
		},
		{
			url:   "https://tw.news.yahoo.com/politics/",
			title: "Synthetic Yahoo Politics Item 1",
			date:  "2026-03-29",
		},
	}

	for _, tt := range tests {
		got, err := registry.Discover(context.Background(), tt.url)
		require.NoError(t, err)
		require.NotEmpty(t, got)
		require.Equal(t, tt.title, got[0].Title)
		require.Equal(t, tt.date, got[0].PublishedAt.Format("2006-01-02"))
	}
}

func fixtureForRequest(req *http.Request) ([]byte, string, error) {
	switch req.URL.Hostname() {
	case "www.dpp.org.tw":
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "synthetic", "discovery", "scout", "dpp_00.html"))
		return body, "text/html; charset=UTF-8", err
	case "www.tpp.org.tw":
		rawPage := req.URL.Query().Get("page")
		if rawPage == "" {
			rawPage = "01"
		}
		page, err := strconv.Atoi(rawPage)
		if err != nil {
			return nil, "", err
		}
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "synthetic", "discovery", "scout", fmt.Sprintf("tpp_%02d.html", page)))
		return body, "text/html; charset=UTF-8", err
	case "feeds.feedburner.com":
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "synthetic", "discovery", "scout", "cna_rss.xml"))
		return body, "application/rss+xml; charset=UTF-8", err
	case "news.pts.org.tw":
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "synthetic", "discovery", "scout", "pts_rss.xml"))
		return body, "application/atom+xml; charset=UTF-8", err
	case "www.kmt.org.tw":
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "synthetic", "discovery", "scout", "kmt_01.xml"))
		return body, "application/atom+xml; charset=UTF-8", err
	case "tw.news.yahoo.com":
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "synthetic", "discovery", "scout", "yahoo_politics.html"))
		return body, "text/html; charset=UTF-8", err
	default:
		return nil, "", fmt.Errorf("unsupported host: %s", req.URL.Hostname())
	}
}
