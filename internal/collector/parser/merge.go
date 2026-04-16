package parser

import (
	"strings"

	"github.com/ChiaYuChang/prism/internal/collector"
)

// MergeArticleContent combines two ArticleContent objects.
// Fields from 'priority' are preferred if they are not zero/empty.
func MergeArticleContent(base, priority *collector.Article) *collector.Article {
	if base == nil {
		return priority
	}
	if priority == nil {
		return base
	}

	merged := *base

	if priority.Title != "" {
		merged.Title = priority.Title
	}
	if priority.Content != "" {
		merged.Content = priority.Content
	}
	if priority.Author != "" {
		merged.Author = priority.Author
	}
	if !priority.PublishedAt.IsZero() {
		merged.PublishedAt = priority.PublishedAt
	}
	if !priority.FetchedAt.IsZero() {
		merged.FetchedAt = priority.FetchedAt
	}

	// Merge metadata maps
	if len(priority.Metadata) > 0 {
		if merged.Metadata == nil {
			merged.Metadata = make(map[string]any)
		}
		for k, v := range priority.Metadata {
			merged.Metadata[k] = v
		}
	}

	// Ensure fields are normalized
	merged.Title = strings.TrimSpace(merged.Title)
	merged.Author = strings.TrimSpace(merged.Author)
	merged.Content = strings.TrimSpace(merged.Content)

	return &merged
}
