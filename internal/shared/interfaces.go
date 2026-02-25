package shared

import (
	"context"
)

// Collector defines a unified interface that composes all the core pipeline
// components: Fetching, Transforming, Parsing, and Saving.
type Collector interface {
	Fetcher
	Transformer
	Parser
	Saver
}

// Fetcher defines the interface for retrieving raw, unprocessed
// data (HTML/JSON) from a remote source.
type Fetcher interface {
	Fetch(ctx context.Context, url string) (string, error)
}

// Transformer responsible for cleaning, minifying, or normalizing raw data
// into a canonical string format that ensures consistency between storage and parsing.
type Transformer interface {
	Transform(ctx context.Context, raw string) (string, error)
}

// Saver handles the physical persistence of normalized archival records to a
// data lake or object storage (e.g., S3/SeaweedFS).
type Saver interface {
	Save(ctx context.Context, record ArchiveRecord) error
}

// Parser extracts structured news metadata and content from normalized canonical
// data into a standardized NewsArticle object.
type Parser interface {
	Parse(ctx context.Context, data string) (*NewsArticle, error)
}

// DiscoveryRepository provides the persistence layer for the Discovery
// service to track URL processing status and store discovered metadata.
type DiscoveryRepository interface {
	IsURLProcessed(ctx context.Context, url string) (bool, error)
	StoreURL(ctx context.Context, url string, source string) error
}
