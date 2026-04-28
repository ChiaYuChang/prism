package parser

import (
	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/pkg/utils"
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
	merged.Title = utils.NormalizeString(merged.Title)
	merged.Author = utils.NormalizeString(merged.Author)
	merged.Content = utils.NormalizeString(merged.Content)

	return &merged
}
