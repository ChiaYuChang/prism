package llm_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector/parser/llm"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestLLMTargetNodeList_Value_JoinsTrimmedNonEmpty(t *testing.T) {
	nl := llm.LLMTargetNodeList{
		{Selector: "h1", Value: "  Title  "},
		{Selector: ".empty", Value: "   "},
		{Selector: "p", Value: "Body"},
	}
	require.Equal(t, "Title\n\nBody", nl.Value())
}

func TestLLMTargetNodeList_Value_AllEmpty(t *testing.T) {
	nl := llm.LLMTargetNodeList{
		{Selector: "x", Value: ""},
		{Selector: "y", Value: "  "},
	}
	require.Equal(t, "", nl.Value())
}

func TestLLMTargetNodeList_Selectors_DropsBlank(t *testing.T) {
	nl := llm.LLMTargetNodeList{
		{Selector: "h1", Value: "v1"},
		{Selector: "  ", Value: "v2"},
		{Selector: ".byline", Value: "v3"},
	}
	require.Equal(t, []string{"h1", ".byline"}, nl.Selectors())
}

func sampleLLMArticleContent() *llm.LLMArticleContent {
	return &llm.LLMArticleContent{
		Title:  llm.LLMTargetNodeList{{Selector: "h1.title", Value: "Hello World"}},
		Author: llm.LLMTargetNodeList{{Selector: ".author", Value: "Alice"}},
		PublishedAt: llm.LLMTargetNodeList{
			{Selector: "time.pub", Value: "2026-04-27"},
		},
		DateLayouts: []string{"2006-01-02"},
		Content: llm.LLMTargetNodeList{
			{Selector: "article p", Value: "First paragraph"},
			{Selector: "article p", Value: "Second paragraph"},
		},
	}
}

func TestLLMArticleContent_ToRuleConfig(t *testing.T) {
	rc := sampleLLMArticleContent().ToRuleConfig()
	require.Equal(t, []string{"h1.title"}, rc.Title)
	require.Equal(t, []string{".author"}, rc.Author)
	require.Equal(t, []string{"time.pub"}, rc.Date)
	require.Equal(t, []string{"article p", "article p"}, rc.Content)
}

func TestLLMArticleContent_ToConfigSnippet_ParsesAsYAMLAndContainsHost(t *testing.T) {
	snippet, err := sampleLLMArticleContent().ToConfigSnippet("example.com")
	require.NoError(t, err)
	require.True(t, strings.Contains(snippet, "example.com:"), "snippet should include host key, got:\n%s", snippet)

	var decoded map[string]map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(snippet), &decoded))
	entry, ok := decoded["example.com"]
	require.True(t, ok)
	require.Equal(t, true, entry["enabled"])
	require.Equal(t, false, entry["jsonld"])
	require.Contains(t, entry, "html")
}

func TestLLMArticleContent_ToArticleContent_ParsesDateWithLayout(t *testing.T) {
	art := sampleLLMArticleContent().ToArticleContent("https://example.com/x")
	require.Equal(t, "https://example.com/x", art.URL)
	require.Equal(t, "Hello World", art.Title)
	require.Equal(t, "Alice", art.Author)
	require.Equal(t, "First paragraph\n\nSecond paragraph", art.Content)
	want, _ := time.Parse("2006-01-02", "2026-04-27")
	require.Equal(t, want, art.PublishedAt)
	require.False(t, art.FetchedAt.IsZero())
}

func TestLLMArticleContent_ToArticleContent_NoDateLayoutMatches(t *testing.T) {
	c := sampleLLMArticleContent()
	c.DateLayouts = []string{"02/01/2006"} // wrong layout for "2026-04-27"
	art := c.ToArticleContent("https://example.com/x")
	require.True(t, art.PublishedAt.IsZero(), "date should not parse with mismatched layout")
}

func TestLLMArticleContent_ToArticleContent_EmptyPublishedAt(t *testing.T) {
	c := sampleLLMArticleContent()
	c.PublishedAt = nil
	art := c.ToArticleContent("https://example.com/x")
	require.True(t, art.PublishedAt.IsZero())
}

// TestLLMArticleContent_UnmarshalRepresentativeJSON guards struct tags and
// field types against accidental rename/retype. The fixture mirrors the shape
// an LLM provider returns under ParserConfigJSONSchema.
func TestLLMArticleContent_UnmarshalRepresentativeJSON(t *testing.T) {
	raw := `{
		"title": [
			{"selector": "h1.headline", "value": "Breaking News"}
		],
		"author": [
			{"selector": ".byline a", "value": "Jane Doe"},
			{"selector": "meta[name=author]", "value": ""}
		],
		"published_at": [
			{"selector": "time[datetime]", "value": "2026-04-27T08:30:00Z"}
		],
		"date_layouts": ["2006-01-02T15:04:05Z07:00", "2006-01-02"],
		"content": [
			{"selector": "article p.lead", "value": "Lead paragraph."},
			{"selector": "article p", "value": "Body paragraph."}
		]
	}`

	var c llm.LLMArticleContent
	require.NoError(t, json.Unmarshal([]byte(raw), &c))

	require.Len(t, c.Title, 1)
	require.Equal(t, "h1.headline", c.Title[0].Selector)
	require.Len(t, c.Author, 2)
	require.Len(t, c.PublishedAt, 1)
	require.Equal(t, []string{"2006-01-02T15:04:05Z07:00", "2006-01-02"}, c.DateLayouts)
	require.Len(t, c.Content, 2)

	rc := c.ToRuleConfig()
	require.Equal(t, []string{"h1.headline"}, rc.Title)
	require.Equal(t, []string{".byline a", "meta[name=author]"}, rc.Author)

	art := c.ToArticleContent("https://news.example.com/story/1")
	require.Equal(t, "Breaking News", art.Title)
	require.Equal(t, "Jane Doe", art.Author)
	require.Equal(t, "Lead paragraph.\n\nBody paragraph.", art.Content)
	want, _ := time.Parse(time.RFC3339, "2026-04-27T08:30:00Z")
	require.Equal(t, want, art.PublishedAt)
}
