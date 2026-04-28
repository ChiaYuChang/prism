package html_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	htmlparser "github.com/ChiaYuChang/prism/internal/collector/parser/html"
	"github.com/ChiaYuChang/prism/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fixtures live in ../_testdata/ (shared across parser/html and parser/jsonld
// since the same HTML page may be parsed by both). They are pinned snapshots
// with hand-extracted expected values — do not regenerate from downloader
// output, or the exact-match assertions below will drift.
//
// To add a new fixture:
//   1. cp testdata/fixtures/<host>/<path> internal/collector/parser/_testdata/<source>_<id>.html
//   2. Open the file, find the h3.news_list_title / div.list_name etc. and
//      manually copy the trimmed text into the expected struct.
//   3. Pick 2-3 short, article-specific phrases for ContentContains.

type expected struct {
	URL             string
	Title           string
	Author          string
	PublishedAt     string // "2006-01-02"
	ContentContains []string
	ContentMinLen   int
}

func TestHTMLParser_DPP(t *testing.T) {
	rules := htmlparser.RuleConfig{
		Title:   []string{"article.news_content h2"},
		Date:    []string{"p.news_content_date"},
		Content: []string{"#media_contents p"},
	}
	dateLayouts := []string{"2006-01-02"}

	cases := []struct {
		name     string
		fixture  string
		expected expected
	}{
		{
			name:    "dpp_11545",
			fixture: "dpp_11545.html",
			expected: expected{
				URL:         "https://www.dpp.org.tw/media/contents/11545",
				Title:       "邊喊要公開透明邊蹺班不開會　民進黨：戳破其「理性監督」的假象，國民黨擺明惡意卡國防",
				Author:      "",
				PublishedAt: "2026-04-21",
				ContentContains: []string{
					"國防部昨日為回應在野黨要求",
					"立委竟全程缺席",
					"開會不來、審預算喊不知情",
					"厚植國家防衛韌性",
				},
				ContentMinLen: 200,
			},
		},
		{
			name:    "dpp_11550",
			fixture: "dpp_11550.html",
			expected: expected{
				URL:         "https://www.dpp.org.tw/media/contents/11550",
				Title:       "鄭麗文再度附和中國「一中政策」　民進黨：與國際脫節、嚴重違背國家路線",
				Author:      "",
				PublishedAt: "2026-04-22",
				ContentContains: []string{
					"賴清德總統出訪受阻",
					"面對中國粗暴施壓",
					"力挺台灣走向國際舞台",
					"違背中華民國台灣優先的國家路線",
				},
				ContentMinLen: 200,
			},
		},
	}

	runCases(t, rules, dateLayouts, cases)
}

func TestHTMLParser_TPP(t *testing.T) {
	rules := htmlparser.RuleConfig{
		Title:   []string{"div.content_topic"},
		Date:    []string{"span.content_date"},
		Content: []string{"div.content_description"},
	}
	dateLayouts := []string{"2006/01/02"}

	cases := []struct {
		name     string
		fixture  string
		expected expected
	}{
		{
			name:    "tpp_4530",
			fixture: "tpp_4530.html",
			expected: expected{
				URL:         "https://www.tpp.org.tw/newsdetail/4530",
				Title:       "【中央評議委員會公告】眾評決字第115000003號",
				Author:      "",
				PublishedAt: "2026-04-14",
				ContentContains: []string{
					"本黨中央委員會移請本會處置",
					"諸多言論與行徑嚴重影響黨聲譽",
					"連續性違紀",
					"嚴重毀損國家公職之公益本質",
					"評議裁決準則第四十八條規定",
				},
				ContentMinLen: 200,
			},
		},
		{
			name:    "tpp_4540",
			fixture: "tpp_4540.html",
			expected: expected{
				URL:         "https://www.tpp.org.tw/newsdetail/4540",
				Title:       "與民同在、彰化我來！民眾彰化隊出發！",
				Author:      "",
				PublishedAt: "2026-04-17",
				ContentContains: []string{
					"彰化縣黨部主委 温宗諭",
					"從資深服務到年輕新血",
					"台灣民眾黨深信",
				},
				ContentMinLen: 200,
			},
		},
	}

	runCases(t, rules, dateLayouts, cases)
}

func TestHTMLParser_KMT(t *testing.T) {
	rules := htmlparser.RuleConfig{
		Title:   []string{"#div1 h3"},
		Date:    []string{"#div1 div.post-footer-line i.pdt abbr.published@title"},
		Content: []string{"#div1 div.post-body p"},
	}
	// KMT is a Blogger site: the visible text under abbr.published is a
	// localized human-readable string ("下午2:00"), but the ISO-8601 stamp
	// lives in the title attribute. Use the "@title" selector suffix to pull
	// it from there.
	dateLayouts := []string{"2006-01-02T15:04:05-07:00"}

	cases := []struct {
		name     string
		fixture  string
		expected expected
	}{
		{
			name:    "kmt_blog-post_20",
			fixture: "kmt_blog-post_20.html",
			expected: expected{
				URL:         "https://www.kmt.org.tw/2026/04/blog-post_20.html",
				Title:       "國民黨呼籲民進黨拋開抗中意識型態的束縛  讓台灣人民共享兩岸交流帶來的實質效益",
				Author:      "",
				PublishedAt: "2026-04-20",
				ContentContains: []string{
					"商總呼籲民進黨政府",
					"涵蓋兩岸直航",
					"攸關無數家庭收入與基層產業存續",
					"農漁產品與食品輸銷等領域",
					"六大重點及相關交流合作事項",
				},
				ContentMinLen: 200,
			},
		},
		{
			name:    "kmt_blog-post_50",
			fixture: "kmt_blog-post_50.html",
			expected: expected{
				URL:         "https://www.kmt.org.tw/2026/04/blog-post_50.html",
				Title:       "民進黨見兩岸利多就抹黑　國民黨：應以民為念，莫讓意識形態凌駕民生",
				Author:      "",
				PublishedAt: "2026-04-12",
				ContentContains: []string{
					"大陸方面宣布十項對台措施",
					"兩岸關係惡化",
					"透過政府既有機制處理",
					"抹黑扯後腿",
				},
				ContentMinLen: 200,
			},
		},
	}

	runCases(t, rules, dateLayouts, cases)
}

func runCases(t *testing.T, rules htmlparser.RuleConfig, dateLayouts []string, cases []struct {
	name     string
	fixture  string
	expected expected
}) {
	t.Helper()

	p := htmlparser.New(rules, dateLayouts)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "_testdata", c.fixture))
			require.NoError(t, err)

			article, err := p.Parse(context.Background(), c.expected.URL, string(body))
			require.NoError(t, err)
			require.NotNil(t, article)

			assert.Equal(t, c.expected.URL, article.URL)
			assert.Equal(t, utils.NormalizeString(c.expected.Title), article.Title, "title mismatch")
			assert.Equal(t, utils.NormalizeString(c.expected.Author), article.Author, "author mismatch")
			assert.Equal(t, c.expected.PublishedAt, article.PublishedAt.Format("2006-01-02"), "published_at mismatch")

			assert.GreaterOrEqual(t, len(article.Content), c.expected.ContentMinLen,
				"content too short: got %d chars, want >= %d", len(article.Content), c.expected.ContentMinLen)

			for _, needle := range c.expected.ContentContains {
				assert.Contains(t, article.Content, utils.NormalizeString(needle),
					"content should contain %q", needle)
			}
		})
	}
}
