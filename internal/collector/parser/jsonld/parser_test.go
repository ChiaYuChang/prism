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
				Title:       "Synthetic CNA News Article",
				Author:      "Synthetic News Desk",
				PublishedAt: "2026-04-22",
				ContentContains: []string{
					"Synthetic CNA body alpha",
					"JSON-LD parser assertions",
					"Synthetic CNA phrase 414",
				},
			},
		},
		{
			name:    "pts_news_article",
			fixture: "pts_804864.html",
			expected: expected{
				URL:         "https://news.pts.org.tw/article/804864",
				Title:       "Synthetic PTS News Article",
				Author:      "Synthetic Reporter",
				PublishedAt: "2026-04-22",
				ContentContains: []string{
					"Synthetic PTS body alpha",
					"JSON-LD parser assertions",
					"Synthetic PTS phrase 804864",
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "synthetic", "collector", "parser", c.fixture))
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
