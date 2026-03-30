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
	cfg, err := scoutconfig.ReadFile(filepath.Join("scouts.yaml"))
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
			title: "日本立民黨眾議員訪民進黨 徐國勇盼深化台日民主聯盟 共促區域和平",
			date:  "2025-09-09",
		},
		{
			url:   "https://www.tpp.org.tw/media?page=1",
			title: "深耕桃園 不分黨派 共同努力！",
			date:  "2025-09-20",
		},
		{
			url:   "https://feeds.feedburner.com/rsscna/politics",
			title: "民進黨啟動地方執政好人才巡迴講座 31日登場",
			date:  "2026-03-29",
		},
		{
			url:   "https://news.pts.org.tw/xml/newsfeed.xml",
			title: "明汽柴油再調漲 中油已吸收逾69.9億元",
			date:  "2026-03-29",
		},
		{
			url:   "https://www.kmt.org.tw/feed",
			title: "朱立倫主席：遲來的交保不是正義，應給柯文哲空間證明清白",
			date:  "2025-09-10",
		},
		{
			url:   "https://tw.news.yahoo.com/politics/",
			title: "民眾黨喊凱道有1萬人！四叉貓曬「人數參照圖」曝真實數字",
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
		body, err := os.ReadFile(filepath.Join("..", "_testdata", "dpp_00.html"))
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
		body, err := os.ReadFile(filepath.Join("..", "_testdata", fmt.Sprintf("tpp_%02d.html", page)))
		return body, "text/html; charset=UTF-8", err
	case "feeds.feedburner.com":
		body, err := os.ReadFile(filepath.Join("..", "_testdata", "cna_rss.xml"))
		return body, "application/rss+xml; charset=UTF-8", err
	case "news.pts.org.tw":
		body, err := os.ReadFile(filepath.Join("..", "_testdata", "pts_rss.xml"))
		return body, "application/atom+xml; charset=UTF-8", err
	case "www.kmt.org.tw":
		body, err := os.ReadFile(filepath.Join("..", "_testdata", "kmt_01.xml"))
		return body, "application/atom+xml; charset=UTF-8", err
	case "tw.news.yahoo.com":
		body, err := os.ReadFile(filepath.Join("..", "_testdata", "yahoo_politics.html"))
		return body, "text/html; charset=UTF-8", err
	default:
		return nil, "", fmt.Errorf("unsupported host: %s", req.URL.Hostname())
	}
}
