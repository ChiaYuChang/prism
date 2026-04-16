package parser

import (
	"context"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/PuerkitoBio/goquery"
)

// ArticleParser extracts structured article content from cleaned HTML
// using common meta tags and semantic selectors.
type ArticleParser struct{}

var _ collector.Parser = (*ArticleParser)(nil)

func NewArticleParser() *ArticleParser {
	return &ArticleParser{}
}

func (p *ArticleParser) Parse(_ context.Context, url string, data string) (*collector.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(data))
	if err != nil {
		return nil, err
	}

	title := extractTitle(doc)
	author := extractAuthor(doc)
	publishedAt := extractPublishedAt(doc)
	content := extractContent(doc)

	return &collector.Article{
		URL:         url,
		Title:       title,
		Content:     content,
		Author:      author,
		PublishedAt: publishedAt,
		FetchedAt:   time.Now(),
	}, nil
}

func extractTitle(doc *goquery.Document) string {
	if t := doc.Find(`meta[property="og:title"]`).AttrOr("content", ""); t != "" {
		return strings.TrimSpace(t)
	}
	if t := doc.Find("h1").First().Text(); t != "" {
		return strings.TrimSpace(t)
	}
	return strings.TrimSpace(doc.Find("title").Text())
}

func extractAuthor(doc *goquery.Document) string {
	candidates := []string{
		doc.Find(`meta[name="author"]`).AttrOr("content", ""),
		doc.Find(`meta[property="article:author"]`).AttrOr("content", ""),
		doc.Find(`[rel="author"]`).First().Text(),
		doc.Find(`.author`).First().Text(),
		doc.Find(`[class*="byline"]`).First().Text(),
	}
	for _, c := range candidates {
		if t := strings.TrimSpace(c); t != "" {
			return t
		}
	}
	return ""
}

func extractPublishedAt(doc *goquery.Document) time.Time {
	candidates := []string{
		doc.Find(`meta[property="article:published_time"]`).AttrOr("content", ""),
		doc.Find(`meta[name="pubdate"]`).AttrOr("content", ""),
		doc.Find(`time[datetime]`).First().AttrOr("datetime", ""),
	}
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02",
	}
	for _, raw := range candidates {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		for _, fmt := range formats {
			if t, err := time.Parse(fmt, raw); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

func extractContent(doc *goquery.Document) string {
	// Try common article containers first.
	for _, sel := range []string{"article", `[role="main"]`, "main", ".article-body", ".post-content", ".entry-content"} {
		if t := strings.TrimSpace(doc.Find(sel).Text()); t != "" {
			return t
		}
	}
	return strings.TrimSpace(doc.Find("body").Text())
}
