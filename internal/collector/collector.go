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
// components: Fetching, Transforming, Parsing, and Saving.
type Collector interface {
	Fetcher
	Transformer
	Parser
	Saver
}

// Fetcher retrieves raw, unprocessed data (HTML/JSON) from a remote source.
// Both the HTTP fetcher (F) and the file recoverer (R) implement this interface.
type Fetcher interface {
	Fetch(ctx context.Context, url string) (string, error)
}

// Transformer applies a string → string transformation to content.
// This single interface covers both minification (strip noise, reduce size)
// and semantic reshaping (type-specific canonicalisation). Which role a
// particular implementation plays is determined by its position in a
// pipeline.Pipeline, not by its type — see docs/pipeline-wiring-design.md.
type Transformer interface {
	Transform(ctx context.Context, in string) (string, error)
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
