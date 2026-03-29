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
	body, err := os.ReadFile(filepath.Join("..", "..", "_testdata", "yahoo_politics.html"))
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
		"https://tw.news.yahoo.com/%E6%B0%91%E7%9C%BE%E9%BB%A8%E5%96%8A%E5%87%B1%E9%81%93%E6%9C%891%E8%90%AC%E4%BA%BA-%E5%9B%9B%E5%8F%89%E8%B2%93%E6%9B%AC-%E4%BA%BA%E6%95%B8%E5%8F%83%E7%85%A7%E5%9C%96-%E6%9B%9D%E7%9C%9F%E5%AF%A6%E6%95%B8%E5%AD%97-070700209.html": {
			Title:     "民眾黨喊凱道有1萬人！四叉貓曬「人數參照圖」曝真實數字",
			PubDate:   "2026-03-29",
			Publisher: "三立新聞網 setn.com",
		},
		"https://tw.news.yahoo.com/%E8%B3%B4%E6%B8%85%E5%BE%B7%E4%BE%86%E4%BA%86-%E4%B8%BB%E6%8C%81%E5%BF%A0%E7%83%88%E6%AE%89%E8%81%B7%E4%BA%BA%E5%93%A1%E8%87%B4%E7%A5%AD%E5%85%B8%E7%A6%AE-%E4%BA%94%E9%99%A2%E9%99%A2%E9%95%B7%E5%94%AF%E7%8D%A8-%E4%BB%96-%E6%9C%AA%E7%8F%BE%E8%BA%AB-022825126.html": {
			Title:     "賴清德來了！主持忠烈殉職人員致祭典禮 五院院長唯獨「他」未現身",
			PubDate:   "2026-03-29",
			Publisher: "民視",
		},
		"https://tw.news.yahoo.com/%E7%B6%A0%E5%A7%94%E8%BD%9F-%E9%99%BD%E5%85%89%E5%A5%B3%E5%AD%90%E5%90%88%E5%94%B1%E5%9C%98-%E5%AE%98%E6%96%B9%E5%BE%AE%E5%8D%9A%E7%A8%B1-%E4%B8%AD%E5%9C%8B%E5%8F%B0%E7%81%A3-%E6%96%87%E5%8C%96%E9%83%A8%E5%9B%9E%E6%87%89%E4%BA%86-060430167.html": {
			Title:     "綠委轟《陽光女子合唱團》官方微博稱「中國台灣」 文化部回應了",
			PubDate:   "2026-03-29",
			Publisher: "台視新聞網",
		},
		"https://tw.stock.yahoo.com/news/%E6%89%93%E4%B8%8D%E8%B4%8F%E4%B9%9F%E8%BC%B8%E4%B8%8D%E8%B5%B7-cnn%E5%88%86%E6%9E%90-%E5%B7%9D%E6%99%AE%E7%9A%84%E4%BC%8A%E6%9C%97%E6%88%B0%E7%88%AD%E6%81%90%E5%8F%AA%E5%89%A9-%E6%A2%9D%E8%B7%AF-124003689.html": {
			Title:     "打不贏也輸不起？CNN分析：川普的伊朗戰爭恐只剩「一條路」",
			PubDate:   "2026-03-28",
			Publisher: "鉅亨網",
		},
		"https://tw.news.yahoo.com/%E6%99%AE%E7%99%BC1%E8%90%AC%E5%85%83-%E4%BB%8A%E5%B9%B4%E5%86%8D%E7%99%BC-%E6%AC%A1-%E8%B2%A1%E6%94%BF%E9%83%A8%E9%95%B7%E5%9B%9E%E6%87%89%E4%BA%86-032000915.html": {
			Title:     "「普發1萬元」今年再發一次？財政部長回應了",
			PubDate:   "2026-03-26",
			Publisher: "三立新聞網 setn.com",
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
	require.Len(t, got, 20)

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
