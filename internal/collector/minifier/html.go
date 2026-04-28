package minifier

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/PuerkitoBio/goquery"
)

// stripSelectors lists selectors for page chrome that are not part of article content.
// Note: <script type="application/ld+json"> is deliberately preserved — it carries
// structured article data consumed by the downstream JSON-LD parser. Stripping all
// <script> tags indiscriminately would blind the parser before it ran.
var stripSelectors = []string{
	`script:not([type="application/ld+json"])`, "style", "noscript",
	"nav", "header", "footer",
	"aside", "iframe", "form",
	"[role=navigation]", "[role=banner]", "[role=complementary]",
	".ad", ".ads", ".advertisement", ".sidebar", ".related",
}

// HTMLMinifier strips non-content elements and returns cleaned HTML
// that preserves the article's semantic structure (headings, paragraphs, lists).
// Implements collector.Transformer; its typical role is the Minifier slot in
// an HTML pipeline (output is the archive point).
type HTMLMinifier struct{}

var _ collector.Transformer = (*HTMLMinifier)(nil)

func New() *HTMLMinifier {
	return &HTMLMinifier{}
}

func (m *HTMLMinifier) Transform(_ context.Context, raw string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}

	for _, sel := range stripSelectors {
		doc.Find(sel).Remove()
	}

	// Remove empty elements that contribute nothing after stripping.
	doc.Find("div, span, section, article").Each(func(_ int, s *goquery.Selection) {
		if strings.TrimSpace(s.Text()) == "" {
			s.Remove()
		}
	})

	html, err := doc.Find("body").Html()
	if err != nil {
		return "", fmt.Errorf("render minified html: %w", err)
	}

	var buf bytes.Buffer
	for _, line := range strings.Split(html, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			buf.WriteString(trimmed)
			buf.WriteByte('\n')
		}
	}
	return buf.String(), nil
}
