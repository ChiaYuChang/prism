package html

import (
	"context"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/PuerkitoBio/goquery"
)

// RuleConfig holds CSS selectors for a single host.
// DateLayouts is deliberately absent — it lives at the ParserConfig level
// so the same layout list applies regardless of parsing method (HTML or JSON-LD).
type RuleConfig struct {
	Title   []string `yaml:"title"   json:"title,omitempty"`
	Author  []string `yaml:"author"  json:"author,omitempty"`
	Date    []string `yaml:"date"    json:"date,omitempty"`
	Content []string `yaml:"content" json:"content,omitempty"`
}

type Parser struct {
	cfg         RuleConfig
	dateLayouts []string
}

var _ collector.Parser = (*Parser)(nil)

// New creates a Parser. dateLayouts is provided separately because it is a
// site-wide concern shared across parsing methods, not specific to HTML selectors.
func New(cfg RuleConfig, dateLayouts []string) *Parser {
	return &Parser{cfg: cfg, dateLayouts: dateLayouts}
}

func (p *Parser) Parse(_ context.Context, url string, data string) (*collector.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(data))
	if err != nil {
		return nil, err
	}

	content := &collector.Article{
		URL:       url,
		FetchedAt: time.Now(),
	}

	content.Title = firstMatch(doc, p.cfg.Title)
	content.Author = firstMatch(doc, p.cfg.Author)
	content.PublishedAt = firstDateMatch(doc, p.cfg.Date, p.dateLayouts)

	var bodyParts []string
	for _, sel := range p.cfg.Content {
		doc.Find(sel).Each(func(_ int, s *goquery.Selection) {
			if text := strings.TrimSpace(s.Text()); text != "" {
				bodyParts = append(bodyParts, text)
			}
		})
		if len(bodyParts) > 0 {
			break
		}
	}
	content.Content = strings.Join(bodyParts, "\n\n")

	return content, nil
}

// firstMatch tries each selector in order and returns the trimmed text of the
// first element that yields a non-empty result.
func firstMatch(doc *goquery.Document, selectors []string) string {
	for _, sel := range selectors {
		if t := strings.TrimSpace(doc.Find(sel).First().Text()); t != "" {
			return t
		}
	}
	return ""
}

// firstDateMatch tries each selector in order; for each non-empty text it
// tries each layout in order and returns the first successfully parsed time.
func firstDateMatch(doc *goquery.Document, selectors, layouts []string) time.Time {
	for _, sel := range selectors {
		text := strings.TrimSpace(doc.Find(sel).First().Text())
		if text == "" {
			continue
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, text); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}
