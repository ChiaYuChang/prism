package parser

import (
	"maps"

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

	if len(priority.Metadata) > 0 {
		// Clone before writing: `merged := *base` is a shallow copy and
		// merged.Metadata still aliases base.Metadata. Writing into it
		// would mutate the caller's input.
		out := make(map[string]any, len(base.Metadata)+len(priority.Metadata))
		maps.Copy(out, base.Metadata)
		maps.Copy(out, priority.Metadata)
		merged.Metadata = out
	}

	// Ensure fields are normalized
	merged.Title = utils.NormalizeString(merged.Title)
	merged.Author = utils.NormalizeString(merged.Author)
	merged.Content = utils.NormalizeString(merged.Content)

	return &merged
}
