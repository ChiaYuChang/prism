package jsonld

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/pkg/utils"
)

// jsonLDPattern matches <script type="application/ld+json">...</script> blocks.
// Handles single/double quotes and arbitrary attribute ordering.
var jsonLDPattern = regexp.MustCompile(
	`(?is)<script[^>]+type=["']application/ld\+json["'][^>]*>([\s\S]*?)</script>`,
)

type Parser struct{}

var _ collector.Parser = (*Parser)(nil)

func New() *Parser {
	return &Parser{}
}

func (p *Parser) Parse(_ context.Context, url string, data string) (*collector.Article, error) {
	var best *collector.Article

	for _, match := range jsonLDPattern.FindAllStringSubmatch(data, -1) {
		raw := strings.TrimSpace(match[1])
		if raw == "" {
			continue
		}

		// Try unmarshaling as a generic object or array
		var ld any
		if err := json.Unmarshal([]byte(raw), &ld); err != nil {
			continue
		}

		processItem := func(item any) {
			if obj, ok := item.(map[string]any); ok {
				if graph, ok := obj["@graph"].([]any); ok {
					for _, g := range graph {
						if gObj, ok := g.(map[string]any); ok {
							if content := extractFromMap(gObj); content != nil {
								best = content
							}
						}
					}
				} else {
					if content := extractFromMap(obj); content != nil {
						best = content
					}
				}
			}
		}

		switch v := ld.(type) {
		case []any:
			for _, item := range v {
				processItem(item)
			}
		case map[string]any:
			processItem(v)
		}
	}

	if best == nil {
		return &collector.Article{URL: url}, nil
	}

	best.URL = url
	return best, nil
}

func extractFromMap(ld map[string]any) *collector.Article {
	typ, _ := ld["@type"].(string)
	if !isArticleType(typ) {
		return nil
	}

	content := &collector.Article{
		Metadata: make(map[string]any),
	}

	if title, ok := ld["headline"].(string); ok {
		content.Title = utils.NormalizeString(title)
	} else if title, ok := ld["name"].(string); ok {
		content.Title = utils.NormalizeString(title)
	}

	if author := extractAuthor(ld["author"]); author != "" {
		content.Author = utils.NormalizeString(author)
	}

	if pubDate := extractDate(ld["datePublished"]); !pubDate.IsZero() {
		content.PublishedAt = pubDate
	}

	if body, ok := ld["articleBody"].(string); ok {
		content.Content = utils.NormalizeString(body)
	}

	if desc, ok := ld["description"].(string); ok {
		content.Metadata["description"] = utils.NormalizeString(desc)
	}

	return content
}

func isArticleType(typ string) bool {
	t := strings.ToLower(typ)
	return strings.Contains(t, "article") || strings.Contains(t, "blogposting")
}

func extractAuthor(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case map[string]any:
		if name, ok := v["name"].(string); ok {
			return name
		}
	case []any:
		if len(v) > 0 {
			return extractAuthor(v[0])
		}
	}
	return ""
}

func extractDate(val any) time.Time {
	str, ok := val.(string)
	if !ok {
		return time.Time{}
	}

	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, str); err == nil {
			return t
		}
	}
	return time.Time{}
}
