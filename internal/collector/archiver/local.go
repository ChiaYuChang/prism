package archiver

import (
	"context"
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

func (a *LocalArchiver) Save(ctx context.Context, record collector.Archive) error {
	dir, err := a.dateDir(record.Timestamp, true)
	if err != nil {
		return err
	}

	dataPath := filepath.Join(dir, record.TraceID+".data")
	if err := os.WriteFile(dataPath, []byte(record.Payload), 0644); err != nil {
		return fmt.Errorf("write payload %s: %w", dataPath, err)
	}

	metaBytes, err := buildMetaJSON(record)
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

	metaPath := strings.TrimSuffix(matches[0], ".data") + ".meta.json"
	m, metaErr := a.readMeta(metaPath)
	if metaErr == nil && m.DeletedAt != nil {
		return "", fmt.Errorf("%w: trace_id=%s (soft-deleted at %s)",
			ErrNotFound, traceID, m.DeletedAt.Format(time.RFC3339))
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		return "", fmt.Errorf("read archive %s: %w", matches[0], err)
	}

	// Integrity check: meta must carry payload_sha256; mismatch or absence → ErrCorrupted.
	if metaErr == nil {
		if m.PayloadSHA256 == "" {
			a.logger.ErrorContext(ctx, "archive meta is missing payload_sha256",
				slog.String("trace_id", traceID),
				slog.String("meta_path", metaPath),
			)
			return "", fmt.Errorf("%w: trace_id=%s (no payload_sha256 in meta)", ErrCorrupted, traceID)
		}
		if actual := sha256Hex(data); actual != m.PayloadSHA256 {
			a.logger.ErrorContext(ctx, "archive integrity check failed",
				slog.String("trace_id", traceID),
				slog.String("expected", m.PayloadSHA256),
				slog.String("actual", actual),
			)
			return "", fmt.Errorf("%w: trace_id=%s", ErrCorrupted, traceID)
		}
	}

	a.logger.DebugContext(ctx, "loaded archive from local filesystem",
		slog.String("trace_id", traceID),
		slog.String("path", matches[0]),
		slog.Int("bytes", len(data)),
	)
	return string(data), nil
}

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
		if !matchesScanOpts(m, opts) {
			continue
		}
		results = append(results, m)
		if opts.Limit > 0 && len(results) >= opts.Limit {
			break
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.Before(results[j].CreatedAt)
	})
	return results, nil
}

// Remove soft-deletes the archive by stamping deleted_at in its .meta.json.
// Idempotent: preserves the original deleted_at timestamp.
func (a *LocalArchiver) Remove(ctx context.Context, traceID string) error {
	pattern := filepath.Join(a.baseDir, "archives", "*", "*", "*", traceID+".data")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob for trace %s: %w", traceID, err)
	}
	if len(matches) == 0 {
		return fmt.Errorf("%w: trace_id=%s", ErrNotFound, traceID)
	}

	now := time.Now().UTC()
	for _, dataPath := range matches {
		metaPath := strings.TrimSuffix(dataPath, ".data") + ".meta.json"
		if err := a.stampDeletedAt(metaPath, now); err != nil {
			return err
		}
		a.logger.DebugContext(ctx, "soft-removed archive",
			slog.String("trace_id", traceID),
			slog.String("meta_file", metaPath),
			slog.Time("deleted_at", now),
		)
	}
	return nil
}

// Purge hard-deletes both .data and .meta.json for a specific traceID.
// The archive must already be soft-deleted — refuses active archives.
func (a *LocalArchiver) Purge(ctx context.Context, traceID string) error {
	pattern := filepath.Join(a.baseDir, "archives", "*", "*", "*", traceID+".data")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob for trace %s: %w", traceID, err)
	}
	if len(matches) == 0 {
		return fmt.Errorf("%w: trace_id=%s", ErrNotFound, traceID)
	}

	for _, dataPath := range matches {
		metaPath := strings.TrimSuffix(dataPath, ".data") + ".meta.json"
		m, err := a.readMeta(metaPath)
		if err != nil {
			return fmt.Errorf("read meta before purge %s: %w", metaPath, err)
		}
		if m.DeletedAt == nil {
			return fmt.Errorf("refusing to purge non-soft-deleted archive: trace_id=%s (call Remove first)", traceID)
		}
		if err := os.Remove(dataPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("purge data file %s: %w", dataPath, err)
		}
		if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("purge meta file %s: %w", metaPath, err)
		}
		a.logger.DebugContext(ctx, "purged archive",
			slog.String("trace_id", traceID),
			slog.Time("deleted_at", *m.DeletedAt),
		)
	}
	return nil
}

// PurgeAll hard-deletes every soft-deleted archive in the tree.
func (a *LocalArchiver) PurgeAll(ctx context.Context) (int, error) {
	pattern := filepath.Join(a.baseDir, "archives", "*", "*", "*", "*.meta.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return 0, fmt.Errorf("glob meta files: %w", err)
	}

	var purged int
	for _, metaPath := range files {
		m, err := a.readMeta(metaPath)
		if err != nil {
			a.logger.WarnContext(ctx, "skipping unreadable meta during PurgeAll",
				slog.String("path", metaPath), slog.Any("error", err))
			continue
		}
		if m.DeletedAt == nil {
			continue
		}
		dataPath := strings.TrimSuffix(metaPath, ".meta.json") + ".data"
		if err := os.Remove(dataPath); err != nil && !os.IsNotExist(err) {
			return purged, fmt.Errorf("purge data file %s: %w", dataPath, err)
		}
		if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
			return purged, fmt.Errorf("purge meta file %s: %w", metaPath, err)
		}
		purged++
	}
	a.logger.InfoContext(ctx, "purged all soft-deleted archives", slog.Int("count", purged))
	return purged, nil
}

// ----- helpers -----

// dateDir returns archives/<YYYY>/<MM>/<DD> under baseDir.
//
// DEPRECATED PATH LAYOUT — see docs/plan/future.md "archive metadata
// catalog separation". Two known issues with the YYYY/MM/DD/<traceID>
// layout: (1) <traceID>.data filename collides when multiple tasks share
// a trace_id (Phase 3 fail-minify run wrote 26 archives but only 3
// survived because seed-tasks.sql uses one trace_id per source); (2) date
// prefix concentrates writes on "today" → S3 hot-prefix throttle risk at
// scale. Future layout: archives/<archive_id> with archive_id as UUID v7
// (date is recoverable from the UUID for debugging).
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
		if !matchesScanOpts(m, opts) {
			continue
		}
		results = append(results, m)
	}
	return results, nil
}

func (a *LocalArchiver) readMeta(path string) (Meta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, fmt.Errorf("read %s: %w", path, err)
	}
	fallback := a.parseTimestampFromPath(path)
	m, err := parseMeta(data, fallback)
	if err != nil {
		return Meta{}, fmt.Errorf("unmarshal %s: %w", path, err)
	}
	return m, nil
}

func (a *LocalArchiver) parseTimestampFromPath(path string) time.Time {
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

func (a *LocalArchiver) stampDeletedAt(metaPath string, t time.Time) error {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return fmt.Errorf("read meta %s: %w", metaPath, err)
	}
	updated, alreadyDeleted, err := stampDeletedAtInJSON(data, t)
	if err != nil {
		return fmt.Errorf("stamp deleted_at in %s: %w", metaPath, err)
	}
	if alreadyDeleted {
		return nil
	}
	if err := os.WriteFile(metaPath, updated, 0644); err != nil {
		return fmt.Errorf("write meta %s: %w", metaPath, err)
	}
	return nil
}
