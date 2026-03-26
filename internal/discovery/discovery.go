package discovery

import (
	"context"

	"github.com/ChiaYuChang/prism/internal/model"
)

// Extractor is responsible for extracting search keywords using AI.
type Extractor interface {
	// ExtractSearchQueries extracts composite search phrases from the input content.
	Extract(ctx context.Context, in *model.ExtractionInput) (out *model.ExtractionOutput, err error)
}

// SearchClient is responsible for communicating with external search engines (e.g., Google).
type SearchClient interface {
	// DiscoverNews executes a search and returns initial media reports and metadata.
	DiscoverNews(ctx context.Context, query string, site string) ([]model.Candidates, error)
}
