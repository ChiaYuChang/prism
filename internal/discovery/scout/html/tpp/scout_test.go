package tpp_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery/scout/html/tpp"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestScoutDiscover(t *testing.T) {
	client := &http.Client{
		Transport: testutils.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			rawPage := req.URL.Query().Get("page")
			if rawPage == "" {
				rawPage = "01"
			}

			page, err := strconv.Atoi(rawPage)
			require.NoError(t, err)

			body, err := os.ReadFile(fmt.Sprintf("../../_testdata/tpp_%02d.html", page))
			require.NoError(t, err)

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(testutils.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}

	items := map[string]struct {
		Title             string
		URL               string
		PubDate           string
		NContentParagraph int
	}{
		"/newsdetail/4190": {
			Title:             "深耕桃園 不分黨派 共同努力！",
			URL:               "/newsdetail/4190",
			PubDate:           "2025-09-20",
			NContentParagraph: 1,
		},
		"/newsdetail/4189": {
			Title:             "空污升級、供電告急，台灣人民到底還要為民進黨錯誤的能源政策付出多少代價？",
			URL:               "/newsdetail/4189",
			PubDate:           "2025-09-19",
			NContentParagraph: 1,
		},
		"/newsdetail/4186": {
			Title:             "台電發電機組接連出包，如果再有意外，恐怕全民都得當墊背。",
			URL:               "/newsdetail/4186",
			PubDate:           "2025-09-18",
			NContentParagraph: 1,
		},
		"/newsdetail/4187": {
			Title:             "國家機器赤裸裸侵犯個人隱私！",
			URL:               "/newsdetail/4187",
			PubDate:           "2025-09-17",
			NContentParagraph: 1,
		},
		"/newsdetail/4188": {
			Title:             "關稅後遺症持續擴大，台灣勞工被迫休養生息。",
			URL:               "/newsdetail/4188",
			PubDate:           "2025-09-17",
			NContentParagraph: 1,
		},
		"/newsdetail/4185": {
			Title:             "民調再創新低的賴清德總統，非常需要掌聲，但台灣人民用民意告訴你，治國需要的不是掌聲。",
			URL:               "/newsdetail/4185",
			PubDate:           "2025-09-16",
			NContentParagraph: 1,
		},
	}

	scout, err := tpp.New(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), client)
	require.NoError(t, err)
	got, err := scout.Discover(context.Background(), "https://www.tpp.org.tw/media?page=1")
	require.NoError(t, err)
	require.Len(t, got, len(items))

	for _, g := range got {
		u, _ := url.Parse(g.URL)

		item, ok := items[u.Path]
		require.True(t, ok, u.Path, "not found")
		require.Equal(t, item.Title, g.Title)
		require.Equal(t, item.PubDate, g.PublishedAt.Format("2006-01-02"))
	}
}
