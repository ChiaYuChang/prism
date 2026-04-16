package collector

import (
	"context"
	"time"
)

// Article is the structured output of the parser pipeline.
// It contains only fields that a parser can populate from raw HTML.
// DB-specific identifiers (source, fingerprint, type) are added by the
// storage layer when persisting to contents.
type Article struct {
	URL         string
	Title       string
	Content     string
	Author      string
	PublishedAt time.Time
	FetchedAt   time.Time
	Metadata    map[string]any
}

// Archive is a raw content record destined for object storage (S3/SeaweedFS).
// It holds the compressed, base64-encoded payload alongside audit metadata.
type Archive struct {
	Fingerprint string
	URL         string
	Payload     string // Gzip + Base64 encoded canonical string
	TraceID     string
	Timestamp   time.Time
	Metadata    map[string]any
}

// Collector defines a unified interface that composes all the core pipeline
// components: Fetching, Minifying, Transforming, Parsing, and Saving.
type Collector interface {
	Fetcher
	Minifier
	Transformer
	Parser
	Saver
}

// Fetcher retrieves raw, unprocessed data (HTML/JSON) from a remote source.
// Both the HTTP fetcher (F) and the file recoverer (R) implement this interface.
type Fetcher interface {
	Fetch(ctx context.Context, url string) (string, error)
}

// Minifier strips noise from raw content and reduces its size.
// Its output is the archive point: the Saver receives minified content on the
// success path, and raw content on the error path.
// Stage 1 of the two-stage transform pipeline.
type Minifier interface {
	Minify(ctx context.Context, raw string) (string, error)
}

// Transformer applies semantic transformations to minified content.
// Currently a no-op for HTML; meaningful for future non-HTML inputs
// such as API responses or structured data formats.
// Stage 2 of the two-stage transform pipeline.
type Transformer interface {
	Transform(ctx context.Context, minified string) (string, error)
}

// Saver persists an Archive record to object storage (SeaweedFS / S3).
// Used on two paths:
//   - success: stores minified content after Minify succeeds
//   - error:   stores raw content when Minify fails, for later replay
type Saver interface {
	Save(ctx context.Context, record Archive) error
}

// Parser extracts a structured Article from canonical HTML.
type Parser interface {
	Parse(ctx context.Context, url string, data string) (*Article, error)
}
