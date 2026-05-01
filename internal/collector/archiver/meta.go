package archiver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
)

// MetaVersion is the current schema version written into every .meta.json file.
// Increment this constant whenever a breaking change is made to the meta format.
//
//	Version 1: url, trace_id, created_at, payload_sha256, prism_metadata, deleted_at
//
// DEPRECATED: do not add new fields to metaFile / Meta. The sidecar meta
// pattern is being replaced by a PG `archives` catalog table — see plan.md
// Future Roadmap "Move archive metadata into PG (catalog + storage
// separation)". New archive-related metadata should be added to the PG
// `contents` / `tasks` tables (or the future `archives` table) instead.
const MetaVersion = 1

type metaFile struct {
	Version       int            `json:"version"`
	URL           string         `json:"url"`
	TraceID       string         `json:"trace_id"`
	CreatedAt     *time.Time     `json:"created_at,omitempty"`
	Fingerprint   string         `json:"fingerprint,omitempty"`
	PayloadSHA256 string         `json:"payload_sha256,omitempty"`
	PrismMetadata map[string]any `json:"prism_metadata,omitempty"`
	DeletedAt     *time.Time     `json:"deleted_at,omitempty"`
}

func buildMetaJSON(record collector.Archive) ([]byte, error) {
	t := record.Timestamp.UTC()
	mf := metaFile{
		Version:       MetaVersion,
		URL:           record.URL,
		TraceID:       record.TraceID,
		CreatedAt:     &t,
		PayloadSHA256: sha256Hex([]byte(record.Payload)),
	}
	if record.Fingerprint != "" {
		mf.Fingerprint = record.Fingerprint
	}
	if len(record.Metadata) > 0 {
		mf.PrismMetadata = record.Metadata
	}
	return json.MarshalIndent(mf, "", "  ")
}

func parseMeta(data []byte, fallbackTime time.Time) (Meta, error) {
	var raw metaFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return Meta{}, err
	}

	createdAt := fallbackTime
	if raw.CreatedAt != nil {
		createdAt = *raw.CreatedAt
	}

	m := Meta{
		Version:       raw.Version,
		TraceID:       raw.TraceID,
		URL:           raw.URL,
		CreatedAt:     createdAt,
		PayloadSHA256: raw.PayloadSHA256,
		DeletedAt:     raw.DeletedAt,
	}
	if pm := raw.PrismMetadata; pm != nil {
		if v, ok := pm["kind"].(string); ok {
			if k, err := ParsePayloadKind(v); err == nil {
				m.PayloadKind = k
			}
		}
		if v, ok := pm["error"].(string); ok {
			m.Error = v
		}
		if v, ok := pm["source_abbr"].(string); ok {
			m.SourceAbbr = v
		}
		if v, ok := pm["source_type"].(string); ok {
			m.SourceType = v
		}
		if v, ok := pm["batch_id"].(string); ok {
			m.BatchID = v
		}
	}
	return m, nil
}

// stampDeletedAtInJSON sets deleted_at in a raw JSON blob.
// Returns (updated JSON, alreadyDeleted, error).
func stampDeletedAtInJSON(data []byte, t time.Time) ([]byte, bool, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, err
	}
	if _, ok := raw["deleted_at"]; ok {
		return data, true, nil
	}
	raw["deleted_at"] = t.Format(time.RFC3339Nano)
	updated, err := json.MarshalIndent(raw, "", "  ")
	return updated, false, err
}

// sha256Hex returns the lowercase hex-encoded SHA-256 digest of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func parseDateFromKeyPath(path string) time.Time {
	idx := strings.Index(path, "archives/")
	if idx < 0 {
		return time.Time{}
	}
	rest := path[idx+len("archives/"):]
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 3 {
		return time.Time{}
	}
	t, err := time.Parse("2006/01/02", parts[0]+"/"+parts[1]+"/"+parts[2])
	if err != nil {
		return time.Time{}
	}
	return t
}

func matchesScanOpts(m Meta, opts ScanOptions) bool {
	if m.DeletedAt != nil && !opts.IncludeRemoved {
		return false
	}
	if !opts.Since.IsZero() && m.CreatedAt.Before(opts.Since) {
		return false
	}
	if !opts.Until.IsZero() && m.CreatedAt.After(opts.Until) {
		return false
	}
	if opts.PayloadKind != "" && m.PayloadKind != opts.PayloadKind {
		return false
	}
	return true
}
