package archiver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
)

// PayloadKind identifies the form of an archived payload (what the bytes are),
// distinct from collector.PipelineStage which identifies pipeline execution
// position. The kind corresponds to the output of a specific pipeline stage.
type PayloadKind string

const (
	PayloadKindRaw       PayloadKind = "raw"       // Fetcher output (pre-Minify)
	PayloadKindMinified  PayloadKind = "minified"  // Minifier output (pre-Transform)
	PayloadKindCanonical PayloadKind = "canonical" // Transformer output (pre-Parse)
)

// ParsePayloadKind converts a string to a PayloadKind, returning an error if
// the string does not match any known kind.
func ParsePayloadKind(s string) (PayloadKind, error) {
	k := PayloadKind(s)
	if k.IsValid() {
		return k, nil
	}
	return "", fmt.Errorf("invalid payload kind: %q", s)
}

func (k PayloadKind) IsValid() bool {
	switch k {
	case PayloadKindRaw, PayloadKindMinified, PayloadKindCanonical:
		return true
	default:
		return false
	}
}

func (k PayloadKind) String() string {
	return string(k)
}

var (
	ErrParamMissing  = errors.New("param missing")
	ErrNotFound      = errors.New("archive not found")
	ErrUnknownScheme = errors.New("unknown archive URI scheme")
	ErrCorrupted     = errors.New("archive payload corrupted") // SHA-256 mismatch on Load
)

// Meta describes a stored archive entry without loading its payload.
type Meta struct {
	Version       int // meta schema version (MetaVersion constant in meta.go)
	TraceID       string
	URL           string
	CreatedAt     time.Time // time Save() was called; second-precision from meta JSON
	PayloadSHA256 string    // hex-encoded SHA-256 of the stored .data file
	PayloadKind   PayloadKind
	Error         string // non-empty when kind=raw (Minify failure, see handler saveOnMinifyError)
	SourceAbbr    string
	SourceType    string
	BatchID       string
	DeletedAt     *time.Time // non-nil = soft-deleted via Remove()
}

// ScanOptions filters the result set returned by Archiver.Scan.
type ScanOptions struct {
	Since          time.Time
	Until          time.Time
	PayloadKind    PayloadKind // "" = all
	Limit          int         // 0 = no limit
	TraceID        string      // non-empty = exact match
	IncludeRemoved bool        // if true, also return soft-deleted entries
}

// Archiver is the full read-write interface for archive storage.
// It embeds collector.Saver so that a single LocalArchiver (or S3Archiver)
// satisfies the narrow errorSaver field in the collector Handler.
//
// DEPRECATED SHAPE — narrowing to bytes-only Save/Load is planned. See
// plan.md Future Roadmap "Move archive metadata into PG (catalog + storage
// separation)". Until that cutover lands:
//   - Do NOT add new callers of Scan or Remove. cmd/recover is the only
//     legitimate Scan caller and is already known.
//   - Do NOT add new fields to Meta (or sidecar meta.json). Push new
//     archive-related metadata into the PG `contents` / `tasks` tables
//     instead so it lands in the right place when the cutover happens.
//   - Path key `archives/YYYY/MM/DD/<traceID>` is also deprecated: it
//     collides when multiple tasks share a trace_id (observed in Phase 3
//     fail-minify run, 26 writes collapsed to 3 surviving files) and
//     concentrates writes on "today" (S3 hot-prefix risk). Future catalog
//     model uses a UUID v7 archive_id as the only path key.
type Archiver interface {
	collector.Saver // Save(ctx context.Context, record collector.Archive) error

	// Load retrieves the payload string for a given traceID.
	// Returns ErrNotFound when no archive matches.
	Load(ctx context.Context, traceID string) (string, error)

	// Scan lists stored archives that match opts.
	// The returned slice is ordered by Timestamp ascending.
	//
	// DEPRECATED: O(N) sidecar reads on local; O(N) GETs on S3. Replace
	// with PG SQL queries against the `archives` catalog table once that
	// lands. New callers: don't.
	Scan(ctx context.Context, opts ScanOptions) ([]Meta, error)

	// Remove deletes the archive for the given traceID.
	// Returns ErrNotFound when no archive matches.
	//
	// DEPRECATED: storage-level soft-delete via meta read-modify-write is
	// not atomic on S3 and reinvents lifecycle policies. Replace with PG
	// `archives.deleted_at` once the catalog lands; payload removal moves
	// to S3 lifecycle / local sweeper. New callers: don't.
	Remove(ctx context.Context, traceID string) error
}
