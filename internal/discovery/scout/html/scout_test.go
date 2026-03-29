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
			configPath: "../config/scouts.yaml",
			rawURL:     "https://www.dpp.org.tw/media",
			transport: func(t *testing.T) http.RoundTripper {
				t.Helper()

				body, err := os.ReadFile(filepath.Join("..", "_testdata", "dpp_00.html"))
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
				"/media/contents/11304": {Title: "日本立民黨眾議員訪民進黨 徐國勇盼深化台日民主聯盟 共促區域和平", PubDate: "2025-09-09"},
				"/media/contents/11303": {Title: "民主黨州主席協會拜會民進黨 徐國勇：台灣人民有守護民主自由的決心", PubDate: "2025-09-09"},
				"/media/contents/11302": {Title: "民進黨：凌濤要求釋放偽造連署黨工，犯了三盲謬誤", PubDate: "2025-09-09"},
				"/media/contents/11301": {Title: "台中垃圾山成市民噩夢 民進黨：市政無能貪污舞弊 盧秀燕別再神隱出來面對", PubDate: "2025-09-08"},
				"/media/contents/11300": {Title: "接見松田康博 徐國勇：民進黨只會越來越團結！", PubDate: "2025-09-05"},
				"/media/contents/11299": {Title: "迎戰預算會期！民進黨辦理新二代實習培訓 為國會注入「新」戰力", PubDate: "2025-09-05"},
				"/media/contents/11298": {Title: "台北3C電器展開幕 徐國勇：感謝電器公會協助南部災區，與民進黨合作挺居民重建生活！", PubDate: "2025-09-05"},
				"/media/contents/11297": {Title: "國民黨放任出席中共93閱兵 民進黨：國民黨不反共棄先烈 弱國防掏空台灣", PubDate: "2025-09-04"},
				"/media/contents/11296": {Title: "賴清德主席與與青年議員對談", PubDate: "2025-09-03"},
				"/media/contents/11295": {Title: "民進黨與自民黨深化交流 召開台日外交防衛政策2+2會談及擴大政策會議 聚焦四大合作領域", PubDate: "2025-09-03"},
			},
		},
		{
			name:       "tpp",
			configPath: "../config/scouts.yaml",
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

					body, err := os.ReadFile(filepath.Join("..", "_testdata", fmt.Sprintf("tpp_%02d.html", page)))
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
				"/newsdetail/4190": {Title: "深耕桃園 不分黨派 共同努力！", PubDate: "2025-09-20"},
				"/newsdetail/4189": {Title: "空污升級、供電告急，台灣人民到底還要為民進黨錯誤的能源政策付出多少代價？", PubDate: "2025-09-19"},
				"/newsdetail/4186": {Title: "台電發電機組接連出包，如果再有意外，恐怕全民都得當墊背。", PubDate: "2025-09-18"},
				"/newsdetail/4187": {Title: "國家機器赤裸裸侵犯個人隱私！", PubDate: "2025-09-17"},
				"/newsdetail/4188": {Title: "關稅後遺症持續擴大，台灣勞工被迫休養生息。", PubDate: "2025-09-17"},
				"/newsdetail/4185": {Title: "民調再創新低的賴清德總統，非常需要掌聲，但台灣人民用民意告訴你，治國需要的不是掌聲。", PubDate: "2025-09-16"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo, err := scoutconfig.Load(tt.configPath)
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
