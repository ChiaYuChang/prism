package archiver

import (
	"context"
	"errors"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
)

var (
	ErrParamMissing  = errors.New("param missing")
	ErrNotFound      = errors.New("archive not found")
	ErrUnknownScheme = errors.New("unknown archive URI scheme")
)

// Meta describes a stored archive entry without loading its payload.
type Meta struct {
	TraceID   string
	URL       string
	Timestamp time.Time
	Stage     string // "raw" or "minified"
	Error     string // non-empty when stage="raw" (minify failure)
}

// ScanOptions filters the result set returned by Archiver.Scan.
type ScanOptions struct {
	Since   time.Time
	Until   time.Time
	Stage   string // "" = all; "raw" = only error archives
	Limit   int    // 0 = no limit
	TraceID string // non-empty = exact match
}

// Archiver is the full read-write interface for archive storage.
// It embeds collector.Saver so that a single LocalArchiver (or S3Archiver)
// satisfies the narrow errorSaver field in the collector Handler.
type Archiver interface {
	collector.Saver // Save(ctx context.Context, record collector.Archive) error

	// Load retrieves the payload string for a given traceID.
	// Returns ErrNotFound when no archive matches.
	Load(ctx context.Context, traceID string) (string, error)

	// Scan lists stored archives that match opts.
	// The returned slice is ordered by Timestamp ascending.
	Scan(ctx context.Context, opts ScanOptions) ([]Meta, error)

	// Remove deletes the archive for the given traceID.
	// Returns ErrNotFound when no archive matches.
	Remove(ctx context.Context, traceID string) error
}
