package parser_test

import (
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/parser"
	"github.com/stretchr/testify/require"
)

func TestMergeArticleContent_NilInputs(t *testing.T) {
	a := &collector.Article{Title: "a"}
	require.Equal(t, a, parser.MergeArticleContent(nil, a))
	require.Equal(t, a, parser.MergeArticleContent(a, nil))
	require.Nil(t, parser.MergeArticleContent(nil, nil))
}

func TestMergeArticleContent_PriorityOverridesScalarsWhenSet(t *testing.T) {
	base := &collector.Article{
		Title:   "base-title",
		Content: "base-content",
		Author:  "base-author",
	}
	priority := &collector.Article{
		Title:  "priority-title",
		Author: "priority-author",
	}

	got := parser.MergeArticleContent(base, priority)

	require.Equal(t, "priority-title", got.Title)
	require.Equal(t, "priority-author", got.Author)
	require.Equal(t, "base-content", got.Content, "empty priority field must not clobber base")
}

func TestMergeArticleContent_PriorityZeroTimesIgnored(t *testing.T) {
	pubBase := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	fetchPriority := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	base := &collector.Article{PublishedAt: pubBase}
	priority := &collector.Article{FetchedAt: fetchPriority}

	got := parser.MergeArticleContent(base, priority)

	require.Equal(t, pubBase, got.PublishedAt, "zero PublishedAt in priority must not clobber base")
	require.Equal(t, fetchPriority, got.FetchedAt)
}

func TestMergeArticleContent_MetadataMergesPriorityWins(t *testing.T) {
	base := &collector.Article{
		Metadata: map[string]any{"a": 1, "b": 2},
	}
	priority := &collector.Article{
		Metadata: map[string]any{"b": 99, "c": 3},
	}

	got := parser.MergeArticleContent(base, priority)

	require.Equal(t, 1, got.Metadata["a"])
	require.Equal(t, 99, got.Metadata["b"], "priority key must overwrite base")
	require.Equal(t, 3, got.Metadata["c"])
}

func TestMergeArticleContent_MetadataInitializedWhenBaseNil(t *testing.T) {
	base := &collector.Article{}
	priority := &collector.Article{
		Metadata: map[string]any{"x": "y"},
	}

	got := parser.MergeArticleContent(base, priority)

	require.NotNil(t, got.Metadata)
	require.Equal(t, "y", got.Metadata["x"])
}

func TestMergeArticleContent_NormalizesStringFields(t *testing.T) {
	base := &collector.Article{
		Title:   "  Base  Title  ",
		Author:  "  Author Name  ",
		Content: "first\n\n\nsecond",
	}

	got := parser.MergeArticleContent(base, &collector.Article{})

	require.Equal(t, "Base Title", got.Title)
	require.Equal(t, "Author Name", got.Author)
	require.NotContains(t, got.Content, "\n\n\n", "Content normalization should collapse runs of newlines")
}

func TestMergeArticleContent_BaseNotMutated(t *testing.T) {
	base := &collector.Article{
		Title:    "base-title",
		Metadata: map[string]any{"a": 1},
	}
	priority := &collector.Article{
		Title:    "priority-title",
		Metadata: map[string]any{"a": 99},
	}

	_ = parser.MergeArticleContent(base, priority)

	require.Equal(t, "base-title", base.Title, "merge must not mutate base scalars")
	require.Equal(t, 1, base.Metadata["a"], "merge must not mutate base.Metadata")
}
