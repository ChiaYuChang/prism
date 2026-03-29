package discovery

import (
	"context"
	"fmt"

	"github.com/ChiaYuChang/prism/internal/model"
)

const ScoutDiscoverSpanNamePattern = "discovery.scout.%s.%s.discover"

func ScoutDiscoverSpanName(format, name string) string {
	return fmt.Sprintf(ScoutDiscoverSpanNamePattern, format, name)
}

// Scout is responsible for discovering news articles.
type Scout interface {
	// DiscoverNews executes a search and returns initial media reports and metadata.
	Discover(ctx context.Context, url string) (out []model.Candidates, err error)
}

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
