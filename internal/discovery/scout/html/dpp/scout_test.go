package dpp_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery/scout/html/dpp"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestScoutDiscover(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "_testdata", "dpp_00.html"))
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
		Title   string
		URL     string
		PubDate string
	}{
		"/media/contents/11304": {
			Title:   "日本立民黨眾議員訪民進黨 徐國勇盼深化台日民主聯盟 共促區域和平",
			URL:     "/media/contents/11304",
			PubDate: "2025-09-09",
		},
		"/media/contents/11303": {
			Title:   "民主黨州主席協會拜會民進黨 徐國勇：台灣人民有守護民主自由的決心",
			URL:     "/media/contents/11303",
			PubDate: "2025-09-09",
		},
		"/media/contents/11302": {
			Title:   "民進黨：凌濤要求釋放偽造連署黨工，犯了三盲謬誤",
			URL:     "/media/contents/11302",
			PubDate: "2025-09-09",
		},
		"/media/contents/11301": {
			Title:   "台中垃圾山成市民噩夢 民進黨：市政無能貪污舞弊 盧秀燕別再神隱出來面對",
			URL:     "/media/contents/11301",
			PubDate: "2025-09-08",
		},
		"/media/contents/11300": {
			Title:   "接見松田康博 徐國勇：民進黨只會越來越團結！",
			URL:     "/media/contents/11300",
			PubDate: "2025-09-05",
		},
		"/media/contents/11299": {
			Title:   "迎戰預算會期！民進黨辦理新二代實習培訓 為國會注入「新」戰力",
			URL:     "/media/contents/11299",
			PubDate: "2025-09-05",
		},
		"/media/contents/11298": {
			Title:   "台北3C電器展開幕 徐國勇：感謝電器公會協助南部災區，與民進黨合作挺居民重建生活！",
			URL:     "/media/contents/11298",
			PubDate: "2025-09-05",
		},
		"/media/contents/11297": {
			Title:   "國民黨放任出席中共93閱兵 民進黨：國民黨不反共棄先烈 弱國防掏空台灣",
			URL:     "/media/contents/11297",
			PubDate: "2025-09-04",
		},
		"/media/contents/11296": {
			Title:   "賴清德主席與與青年議員對談",
			URL:     "/media/contents/11296",
			PubDate: "2025-09-03",
		},
		"/media/contents/11295": {
			Title:   "民進黨與自民黨深化交流 召開台日外交防衛政策2+2會談及擴大政策會議 聚焦四大合作領域",
			URL:     "/media/contents/11295",
			PubDate: "2025-09-03",
		},
	}

	scout, err := dpp.New(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), client)
	require.NoError(t, err)
	got, err := scout.Discover(context.Background(), "https://www.dpp.org.tw/media")
	require.NoError(t, err)
	require.Len(t, got, len(items))

	for _, g := range got {
		u, err := url.Parse(g.URL)
		require.NoError(t, err)

		item, ok := items[u.Path]
		require.True(t, ok, u.Path, "not found")
		require.Equal(t, item.Title, g.Title)
		require.Equal(t, item.PubDate, g.PublishedAt.Format("2006-01-02"))
	}
}
