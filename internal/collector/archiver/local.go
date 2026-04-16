package archiver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
)

// LocalArchiver stores and retrieves archives on the local filesystem.
//
// Layout:
//
//	{baseDir}/archives/{YYYY}/{MM}/{DD}/{traceID}.data       payload
//	{baseDir}/archives/{YYYY}/{MM}/{DD}/{traceID}.meta.json  metadata
//
// It satisfies both collector.Saver (for use as errorSaver in the collector
// Handler) and the full Archiver interface (for cmd/recover).
type LocalArchiver struct {
	baseDir string
	logger  *slog.Logger
}

var _ Archiver = (*LocalArchiver)(nil)
var _ collector.Saver = (*LocalArchiver)(nil)

// NewLocalArchiver creates a LocalArchiver rooted at baseDir.
// baseDir is created (with all parents) if it does not already exist.
func NewLocalArchiver(baseDir string, logger *slog.Logger) (*LocalArchiver, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("%w: baseDir", ErrParamMissing)
	}
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create base directory %s: %w", baseDir, err)
	}
	return &LocalArchiver{baseDir: baseDir, logger: logger}, nil
}

// Save persists record.Payload to disk as {traceID}.data alongside a
// {traceID}.meta.json file containing URL, fingerprint, and custom metadata.
func (a *LocalArchiver) Save(ctx context.Context, record collector.Archive) error {
	dir, err := a.dateDir(record.Timestamp, true)
	if err != nil {
		return err
	}

	dataPath := filepath.Join(dir, record.TraceID+".data")
	if err := os.WriteFile(dataPath, []byte(record.Payload), 0644); err != nil {
		return fmt.Errorf("write payload %s: %w", dataPath, err)
	}

	meta := map[string]any{
		"url":      record.URL,
		"trace_id": record.TraceID,
	}
	if record.Fingerprint != "" {
		meta["fingerprint"] = record.Fingerprint
	}
	if len(record.Metadata) > 0 {
		meta["prism_metadata"] = record.Metadata
	}

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata for %s: %w", record.TraceID, err)
	}

	metaPath := filepath.Join(dir, record.TraceID+".meta.json")
	if err := os.WriteFile(metaPath, metaBytes, 0644); err != nil {
		return fmt.Errorf("write metadata %s: %w", metaPath, err)
	}

	a.logger.DebugContext(ctx, "archived to local filesystem",
		slog.String("data_file", dataPath),
		slog.String("meta_file", metaPath),
	)
	return nil
}

// Load retrieves the payload stored under traceID.
// It globs {baseDir}/archives/*/*/{traceID}.data so the caller does not need
// to know the date subdirectory.
// Returns ErrNotFound when no matching file exists.
func (a *LocalArchiver) Load(ctx context.Context, traceID string) (string, error) {
	pattern := filepath.Join(a.baseDir, "archives", "*", "*", "*", traceID+".data")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob for trace %s: %w", traceID, err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("%w: trace_id=%s", ErrNotFound, traceID)
	}
	if len(matches) > 1 {
		a.logger.WarnContext(ctx, "multiple archive files found for trace ID, using first",
			slog.String("trace_id", traceID),
			slog.Int("count", len(matches)),
			slog.String("using", matches[0]),
		)
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		return "", fmt.Errorf("read archive %s: %w", matches[0], err)
	}

	a.logger.DebugContext(ctx, "loaded archive from local filesystem",
		slog.String("trace_id", traceID),
		slog.String("path", matches[0]),
		slog.Int("bytes", len(data)),
	)
	return string(data), nil
}

// Scan lists archive metadata entries that match opts.
// Results are ordered by Timestamp ascending.
func (a *LocalArchiver) Scan(ctx context.Context, opts ScanOptions) ([]Meta, error) {
	if opts.TraceID != "" {
		return a.scanByTraceID(ctx, opts)
	}

	pattern := filepath.Join(a.baseDir, "archives", "*", "*", "*", "*.meta.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob meta files: %w", err)
	}

	var results []Meta
	for _, f := range files {
		m, err := a.readMeta(f)
		if err != nil {
			a.logger.WarnContext(ctx, "skipping unreadable meta file", slog.String("path", f), slog.Any("error", err))
			continue
		}
		if !a.matchesScanOpts(m, opts) {
			continue
		}
		results = append(results, m)
		if opts.Limit > 0 && len(results) >= opts.Limit {
			break
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.Before(results[j].Timestamp)
	})
	return results, nil
}

// Remove deletes both the .data and .meta.json files for traceID.
// Returns ErrNotFound when no matching data file exists.
func (a *LocalArchiver) Remove(ctx context.Context, traceID string) error {
	pattern := filepath.Join(a.baseDir, "archives", "*", "*", "*", traceID+".data")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob for trace %s: %w", traceID, err)
	}
	if len(matches) == 0 {
		return fmt.Errorf("%w: trace_id=%s", ErrNotFound, traceID)
	}

	for _, dataPath := range matches {
		if err := os.Remove(dataPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove data file %s: %w", dataPath, err)
		}
		metaPath := strings.TrimSuffix(dataPath, ".data") + ".meta.json"
		if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove meta file %s: %w", metaPath, err)
		}
		a.logger.DebugContext(ctx, "removed archive",
			slog.String("trace_id", traceID),
			slog.String("data_file", dataPath),
		)
	}
	return nil
}

// ----- helpers -----

func (a *LocalArchiver) dateDir(t time.Time, create bool) (string, error) {
	dir := filepath.Join(a.baseDir, "archives", t.Format("2006/01/02"))
	if create {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("create archive dir %s: %w", dir, err)
		}
	}
	return dir, nil
}

func (a *LocalArchiver) scanByTraceID(ctx context.Context, opts ScanOptions) ([]Meta, error) {
	pattern := filepath.Join(a.baseDir, "archives", "*", "*", "*", opts.TraceID+".meta.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob for trace %s: %w", opts.TraceID, err)
	}
	var results []Meta
	for _, f := range files {
		m, err := a.readMeta(f)
		if err != nil {
			a.logger.WarnContext(ctx, "skipping unreadable meta file", slog.String("path", f), slog.Any("error", err))
			continue
		}
		if !a.matchesScanOpts(m, opts) {
			continue
		}
		results = append(results, m)
	}
	return results, nil
}

type localMetaFile struct {
	URL          string         `json:"url"`
	TraceID      string         `json:"trace_id"`
	PrismMetadata map[string]any `json:"prism_metadata,omitempty"`
}

func (a *LocalArchiver) readMeta(path string) (Meta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, fmt.Errorf("read %s: %w", path, err)
	}
	var raw localMetaFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return Meta{}, fmt.Errorf("unmarshal %s: %w", path, err)
	}

	// Derive Timestamp from the date path segment: .../archives/YYYY/MM/DD/...
	ts := a.parseTimestampFromPath(path)

	m := Meta{
		TraceID:   raw.TraceID,
		URL:       raw.URL,
		Timestamp: ts,
	}
	if pm := raw.PrismMetadata; pm != nil {
		if stage, ok := pm["stage"].(string); ok {
			m.Stage = stage
		}
		if errStr, ok := pm["error"].(string); ok {
			m.Error = errStr
		}
	}
	return m, nil
}

func (a *LocalArchiver) parseTimestampFromPath(path string) time.Time {
	// path = {baseDir}/archives/{YYYY}/{MM}/{DD}/{traceID}.meta.json
	rel, err := filepath.Rel(filepath.Join(a.baseDir, "archives"), path)
	if err != nil {
		return time.Time{}
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 4 {
		return time.Time{}
	}
	t, err := time.Parse("2006/01/02", strings.Join(parts[:3], "/"))
	if err != nil {
		return time.Time{}
	}
	return t
}

func (a *LocalArchiver) matchesScanOpts(m Meta, opts ScanOptions) bool {
	if !opts.Since.IsZero() && m.Timestamp.Before(opts.Since) {
		return false
	}
	if !opts.Until.IsZero() && m.Timestamp.After(opts.Until) {
		return false
	}
	if opts.Stage != "" && m.Stage != opts.Stage {
		return false
	}
	return true
}
