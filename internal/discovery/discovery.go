package discovery

import (
	"context"

	"github.com/ChiaYuChang/prism/internal/model"
)

// KeywordExtractor is responsible for extracting search keywords using AI.
type KeywordExtractor interface {
	// ExtractSearchQueries extracts composite search phrases from the input content.
	ExtractSearchQueries(ctx context.Context, content string) ([]string, error)
}

// SearchClient is responsible for communicating with external search engines (e.g., Google).
type SearchClient interface {
	// DiscoverNews executes a search and returns initial media reports and metadata.
	DiscoverNews(ctx context.Context, query string, site string) ([]model.ArticleFingerprint, error)
}
