package llm

import (
	"fmt"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/parser/html"
	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
	"github.com/google/jsonschema-go/jsonschema"
	"gopkg.in/yaml.v3"
)

type LLMTargetNode struct {
	Selector string `json:"selector"`
	Value    string `json:"value"`
}

type LLMTargetNodeList []LLMTargetNode

func (nl LLMTargetNodeList) Value() string {
	var parts []string
	for _, n := range nl {
		if text := strings.TrimSpace(n.Value); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (nl LLMTargetNodeList) Selectors() []string {
	var selectors []string
	for _, n := range nl {
		if sel := strings.TrimSpace(n.Selector); sel != "" {
			selectors = append(selectors, sel)
		}
	}
	return selectors
}

type LLMArticleContent struct {
	Title       LLMTargetNodeList `json:"title"`
	Author      LLMTargetNodeList `json:"author"`
	PublishedAt LLMTargetNodeList `json:"published_at"`
	DateLayouts []string          `json:"date_layouts"`
	Content     LLMTargetNodeList `json:"content"`
}

func (llm *LLMArticleContent) ToRuleConfig() html.RuleConfig {
	return html.RuleConfig{
		Title:   llm.Title.Selectors(),
		Author:  llm.Author.Selectors(),
		Date:    llm.PublishedAt.Selectors(),
		Content: llm.Content.Selectors(),
	}
}

// configSnippetEntry mirrors the parser config YAML structure for a single host.
type configSnippetEntry struct {
	Enabled     *bool           `yaml:"enabled"`
	JSONLD      bool            `yaml:"jsonld"`
	DateLayouts []string        `yaml:"date_layouts,omitempty"`
	HTML        html.RuleConfig `yaml:"html"`
}

// ToConfigSnippet generates a YAML fragment for human review.
// The output is suitable for pasting under the `parsers:` key in parsers.yaml
// after an engineer verifies the selectors are stable across multiple pages.
func (llm *LLMArticleContent) ToConfigSnippet(host string) (string, error) {
	t := true
	entry := configSnippetEntry{
		Enabled:     &t,
		JSONLD:      false,
		DateLayouts: llm.DateLayouts,
		HTML:        llm.ToRuleConfig(),
	}
	b, err := yaml.Marshal(map[string]configSnippetEntry{host: entry})
	if err != nil {
		return "", fmt.Errorf("marshal config snippet: %w", err)
	}
	return string(b), nil
}

func (llm *LLMArticleContent) ToArticleContent(url string) *collector.Article {
	article := &collector.Article{
		URL:       url,
		FetchedAt: time.Now(),
		Title:     llm.Title.Value(),
		Author:    llm.Author.Value(),
		Content:   llm.Content.Value(),
	}

	// Try each published_at node's value against each layout until one parses.
	for _, node := range llm.PublishedAt {
		dateStr := strings.TrimSpace(node.Value)
		if dateStr == "" {
			continue
		}
		for _, layout := range llm.DateLayouts {
			if t, err := time.Parse(layout, dateStr); err == nil {
				article.PublishedAt = t
				break
			}
		}
		if !article.PublishedAt.IsZero() {
			break
		}
	}

	return article
}

// newTargetNodeSchema builds a fresh {selector, value} subschema. A function
// rather than a shared pointer because the jsonschema validator requires
// schemas to form a tree — one *jsonschema.Schema cannot appear under two
// distinct paths in the document.
func newTargetNodeSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:        "object",
		Description: "A DOM node mapping: the CSS selector that targets the element and the extracted text value.",
		Required:    []string{"selector", "value"},
		Properties: map[string]*jsonschema.Schema{
			"selector": {
				Type:        "string",
				Description: "CSS selector targeting this element.",
			},
			"value": {
				Type:        "string",
				Description: "Exact text extracted by this selector.",
			},
		},
		PropertyOrder: []string{"selector", "value"},
	}
}

var ParserConfigJSONSchema = func() pkgschema.JSONSchema {
	s := pkgschema.NewSkeleton[LLMArticleContent]("parser_config", 1)
	s.Title = "Article Parser Output"
	s.Description = "Extracted article content with CSS selectors for future rule-based parser configuration."
	s.Required = []string{"title", "author", "published_at", "date_layouts", "content"}

	s.Properties["title"].Description = "Selector/value pairs for the article headline, in priority order."
	s.Properties["title"].Items = newTargetNodeSchema()

	s.Properties["author"].Description = "Selector/value pairs for the article author(s), in priority order."
	s.Properties["author"].Items = newTargetNodeSchema()

	s.Properties["published_at"].Description = "Selector/value pairs for the publish date element, in priority order."
	s.Properties["published_at"].Items = newTargetNodeSchema()

	s.Properties["date_layouts"].Description = "Go time.Parse layout strings to try when parsing the date value, in priority order (e.g. '2006-01-02T15:04:05Z07:00', '2006-01-02')."
	s.Properties["date_layouts"].Items = &jsonschema.Schema{Type: "string"}

	s.Properties["content"].Description = "Selector/value pairs for the article body. Array entries are fallback tiers — use a comma-separated multi-selector (e.g. 'p.a, p.b') to preserve DOM order when content spans interleaved elements."
	s.Properties["content"].Items = newTargetNodeSchema()

	return s
}()
