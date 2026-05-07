package html

import (
	"context"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/pkg/utils"
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

func (*Parser) String() string { return "HTMLParser" }

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
			if text := utils.NormalizeString(s.Text()); text != "" {
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

// splitSelectorAttr splits an optional "@attr" suffix from a selector.
// "div.x"         → ("div.x", "")       → read element text
// "abbr@title"    → ("abbr", "title")   → read title attribute
// Attribute-reading is needed for sites like Blogger where the visible text
// is human-readable ("下午2:00") but the machine-readable ISO timestamp lives
// in an attribute (abbr.published[title]).
func splitSelectorAttr(s string) (selector, attr string) {
	if i := strings.LastIndex(s, "@"); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// extractOne finds the first matching element and returns its text (or named
// attribute value if the selector has an "@attr" suffix), normalized.
func extractOne(doc *goquery.Document, rawSel string) string {
	sel, attr := splitSelectorAttr(rawSel)
	node := doc.Find(sel).First()
	if node.Length() == 0 {
		return ""
	}
	if attr != "" {
		return utils.NormalizeString(node.AttrOr(attr, ""))
	}
	return utils.NormalizeString(node.Text())
}

// firstMatch tries each selector in order and returns the first non-empty
// extracted value (text or "@attr" value).
func firstMatch(doc *goquery.Document, selectors []string) string {
	for _, sel := range selectors {
		if t := extractOne(doc, sel); t != "" {
			return t
		}
	}
	return ""
}

// firstDateMatch tries each selector in order; for each non-empty extracted
// value it tries each layout in order and returns the first parsed time.
func firstDateMatch(doc *goquery.Document, selectors, layouts []string) time.Time {
	for _, sel := range selectors {
		text := extractOne(doc, sel)
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
