package jsonld_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector/parser/jsonld"
	"github.com/ChiaYuChang/prism/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type expected struct {
	URL             string
	Title           string
	Author          string
	PublishedAt     string // "2006-01-02"
	ContentContains []string
}

func TestJSONLDParser(t *testing.T) {
	p := jsonld.New()

	cases := []struct {
		name     string
		fixture  string
		expected expected
	}{
		{
			name:    "cna_news_article",
			fixture: "cna_202604220414.aspx",
			expected: expected{
				URL:         "https://www.cna.com.tw/news/aipl/202604220414.aspx",
				Title:       "時隔7年部會首長登太平島 管碧玲主持南援九號演練",
				Author:      "中央通訊社 Central News Agency",
				PublishedAt: "2026-04-22",
				ContentContains: []string{
					"海委會海巡署與國防部等多個機關",
					"南援九號",
					"管碧玲首次登島",
					"科技整合能力與整體應變能量",
				},
			},
		},
		{
			name:    "pts_news_article",
			fixture: "pts_804864.html",
			expected: expected{
				URL:         "https://news.pts.org.tw/article/804864",
				Title:       "英國議會通過世代禁菸令 2009年後出生者終身不得買菸 ｜ 公視新聞網 PNN",
				Author:      "蔡思培",
				PublishedAt: "2026-04-22",
				ContentContains: []string{
					"英國議會21日通過一項世代禁菸法案",
					"新法案也將同步擴大禁菸場所至學校",
					"電子煙及尼古丁商品的品牌推廣與廣告",
					"紐國新政府廢除",
				},
			},
		},
	}

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

			for _, needle := range c.expected.ContentContains {
				assert.Contains(t,
					utils.NormalizeString(article.Content),
					utils.NormalizeString(needle),
					"content should contain %q", needle)
			}
		})
	}
}
