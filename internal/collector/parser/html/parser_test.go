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

// Fixtures live in testdata/synthetic/collector/parser. They are small,
// hand-written pages that preserve selector shape without copying third-party
// website text.
//
// To add a new fixture:
//   1. Add a synthetic page under testdata/synthetic/collector/parser.
//   2. Preserve the target site's selector structure with original fake text.
//   3. Pick 2-3 short, synthetic phrases for ContentContains.

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
				Title:       "Synthetic DPP Article 11545",
				Author:      "",
				PublishedAt: "2026-04-21",
				ContentContains: []string{
					"Synthetic DPP paragraph alpha",
					"verify extraction length",
					"Synthetic DPP phrase 11545",
				},
				ContentMinLen: 200,
			},
		},
		{
			name:    "dpp_11550",
			fixture: "dpp_11550.html",
			expected: expected{
				URL:         "https://www.dpp.org.tw/media/contents/11550",
				Title:       "Synthetic DPP Article 11550",
				Author:      "",
				PublishedAt: "2026-04-22",
				ContentContains: []string{
					"Synthetic DPP paragraph gamma",
					"copyright safe",
					"Synthetic DPP phrase 11550",
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
				Title:       "Synthetic TPP Announcement 4530",
				Author:      "",
				PublishedAt: "2026-04-14",
				ContentContains: []string{
					"Synthetic TPP paragraph alpha",
					"verify extraction length",
					"Synthetic TPP phrase 4530",
				},
				ContentMinLen: 200,
			},
		},
		{
			name:    "tpp_4540",
			fixture: "tpp_4540.html",
			expected: expected{
				URL:         "https://www.tpp.org.tw/newsdetail/4540",
				Title:       "Synthetic TPP Announcement 4540",
				Author:      "",
				PublishedAt: "2026-04-17",
				ContentContains: []string{
					"Synthetic TPP paragraph gamma",
					"safe to commit",
					"Synthetic TPP phrase 4540",
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
				Title:       "Synthetic KMT Blog Post 20",
				Author:      "",
				PublishedAt: "2026-04-20",
				ContentContains: []string{
					"Synthetic KMT paragraph alpha",
					"verify extraction length",
					"Synthetic KMT phrase post 20",
				},
				ContentMinLen: 200,
			},
		},
		{
			name:    "kmt_blog-post_50",
			fixture: "kmt_blog-post_50.html",
			expected: expected{
				URL:         "https://www.kmt.org.tw/2026/04/blog-post_50.html",
				Title:       "Synthetic KMT Blog Post 50",
				Author:      "",
				PublishedAt: "2026-04-12",
				ContentContains: []string{
					"Synthetic KMT paragraph gamma",
					"safe to commit",
					"Synthetic KMT phrase post 50",
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
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "synthetic", "collector", "parser", c.fixture))
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
