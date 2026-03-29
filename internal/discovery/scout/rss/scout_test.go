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
				"https://www.cna.com.tw/news/aipl/202603290080.aspx": {Title: "民進黨啟動地方執政好人才巡迴講座　31日登場", PubDate: "2026-03-29"},
				"https://www.cna.com.tw/news/aipl/202603290072.aspx": {Title: "114年來台旅客日本最多　香港韓國均破百萬人次", PubDate: "2026-03-29"},
				"https://www.cna.com.tw/news/aipl/202603290071.aspx": {Title: "政院：地方首長應與立委溝通　儘速完成總預算審議", PubDate: "2026-03-29"},
				"https://www.cna.com.tw/news/aipl/202603290063.aspx": {Title: "綠：中共用AI進化認知作戰　提升媒體識讀是防線", PubDate: "2026-03-29"},
				"https://www.cna.com.tw/news/aipl/202603290053.aspx": {Title: "賴總統赴忠烈祠主持春祭　立法院長韓國瑜缺席", PubDate: "2026-03-29"},
				"https://www.cna.com.tw/news/aipl/202603290037.aspx": {Title: "蘇巧慧：初選完成　啟動第2輪掃市場拜票活動", PubDate: "2026-03-29"},
				"https://www.cna.com.tw/news/aipl/202603290022.aspx": {Title: "9艘共艦13架次共機擾台　國軍監控", PubDate: "2026-03-29"},
				"https://www.cna.com.tw/news/aipl/202603290007.aspx": {Title: "駐英副代表江雅綺分享台灣經驗　聚焦經濟安全韌性", PubDate: "2026-03-29"},
				"https://www.cna.com.tw/news/aipl/202603280221.aspx": {Title: "范雲赴馬尼拉演說　訴求亞洲國家團結抗中", PubDate: "2026-03-28"},
				"https://www.cna.com.tw/news/aipl/202603280205.aspx": {Title: "塑膠袋缺貨潮　政院：零售庫存充足、備妥產能彈性增產", PubDate: "2026-03-28"},
				"https://www.cna.com.tw/news/aipl/202603280198.aspx": {Title: "國民黨新竹縣長初選徐欣瑩勝出　陳見賢喊加油", PubDate: "2026-03-28"},
				"https://www.cna.com.tw/news/aipl/202603280197.aspx": {Title: "谷立言：美對台政策未變　支持政院版國防特別預算", PubDate: "2026-03-28"},
				"https://www.cna.com.tw/news/aipl/202603280179.aspx": {Title: "陸軍司令部舉辦高中儀隊初賽　新興高中奪北區冠軍", PubDate: "2026-03-28"},
				"https://www.cna.com.tw/news/aipl/202603280169.aspx": {Title: "政院：汽油30日零時調漲1.7元　政府吸收9.2元", PubDate: "2026-03-28"},
				"https://www.cna.com.tw/news/aipl/202603280165.aspx": {Title: "卓榮泰籲在野黨支持國防　藍：堅持專業審查特別預算", PubDate: "2026-03-28"},
				"https://www.cna.com.tw/news/aipl/202603280131.aspx": {Title: "韓瑩返鄉參選宜蘭市長　盼提供國際專業化新選項", PubDate: "2026-03-28"},
				"https://www.cna.com.tw/news/aipl/202603280129.aspx": {Title: "傳民進黨勸進參選新竹市長　莊競程證實獲徵詢", PubDate: "2026-03-28"},
				"https://www.cna.com.tw/news/aipl/202603280121.aspx": {Title: "國防部：11架次共機越中線配合共艦擾台　國軍嚴密掌握", PubDate: "2026-03-28"},
				"https://www.cna.com.tw/news/aipl/202603280111.aspx": {Title: "外交部志工感謝茶會　吳志中：體現人人都是外交官", PubDate: "2026-03-28"},
				"https://www.cna.com.tw/news/aipl/202603280103.aspx": {Title: "金融時報：美參議員將訪台　促立法院通過國防特別預算", PubDate: "2026-03-28"},
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
				"https://news.ttv.com.tw/news/11503290001600W": {Title: "屏東4.7「空白區」地震！氣象署示警：未來3天當心餘震", PubDate: "2026-03-29"},
				"https://news.ttv.com.tw/news/11503290010100I": {Title: "出國注意！旅遊不便險新制4月上路 定額給付一人限買2張", PubDate: "2026-03-29"},
				"https://news.ttv.com.tw/news/11503290001300W": {Title: "蔡壁如喊柯文哲回鍋選黨主席 黃國昌：「老大」只要點頭隨時回來接任", PubDate: "2026-03-29"},
				"https://news.ttv.com.tw/news/11503290001500W": {Title: "中職／精準預測WBC中華隊戰果！ 爆紅神犬「萊芙」4/4現身新莊開球", PubDate: "2026-03-29"},
				"https://news.ttv.com.tw/news/11503290001200W": {Title: "綠委轟《陽光女子合唱團》官方微博稱「中國台灣」 文化部回應了", PubDate: "2026-03-29"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body, err := os.ReadFile(filepath.Join("..", "_testdata", tt.fixture))
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
