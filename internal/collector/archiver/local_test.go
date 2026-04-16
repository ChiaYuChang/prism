package archiver_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/require"
)

func TestLocalArchiver_SaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	record := collector.Archive{
		URL:       "https://example.com/article",
		Payload:   "<html>saved content</html>",
		TraceID:   "trace-abc123",
		Timestamp: time.Now(),
	}
	require.NoError(t, a.Save(ctx, record))

	got, err := a.Load(ctx, "trace-abc123")
	require.NoError(t, err)
	require.Equal(t, "<html>saved content</html>", got)
}

func TestLocalArchiver_Load_NotFound(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	_, err = a.Load(context.Background(), "nonexistent-trace")
	require.Error(t, err)
	require.True(t, errors.Is(err, archiver.ErrNotFound))
}

func TestLocalArchiver_Load_SelectsCorrectFile(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()

	for _, tc := range []struct{ traceID, payload string }{
		{"trace-first", "first content"},
		{"trace-second", "second content"},
	} {
		require.NoError(t, a.Save(ctx, collector.Archive{
			URL:       "https://example.com/" + tc.traceID,
			Payload:   tc.payload,
			TraceID:   tc.traceID,
			Timestamp: now,
		}))
	}

	got, err := a.Load(ctx, "trace-first")
	require.NoError(t, err)
	require.Equal(t, "first content", got)

	got, err = a.Load(ctx, "trace-second")
	require.NoError(t, err)
	require.Equal(t, "second content", got)
}

func TestNewLocalArchiver_RequiresBaseDir(t *testing.T) {
	_, err := archiver.NewLocalArchiver("", testutils.Logger())
	require.Error(t, err)
}

func TestNewLocalArchiver_RequiresLogger(t *testing.T) {
	_, err := archiver.NewLocalArchiver(t.TempDir(), nil)
	require.Error(t, err)
}

// verifyArchiveDirLayout confirms LocalArchiver writes to the expected path.
// The glob pattern in Load must stay in sync with this layout.
func TestLocalArchiver_Layout(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	now := time.Now()
	require.NoError(t, a.Save(context.Background(), collector.Archive{
		URL: "https://x.com", Payload: "p", TraceID: "my-trace", Timestamp: now,
	}))

	expected := filepath.Join(dir, "archives", now.Format("2006/01/02"), "my-trace.data")
	_, err = os.Stat(expected)
	require.NoError(t, err, "LocalArchiver must write to {baseDir}/archives/{YYYY/MM/DD}/{traceID}.data")
}

func TestLocalArchiver_Scan_FilterByStage(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()

	// Save one "raw" archive (minify error path) and one with no stage set.
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://example.com/raw",
		Payload:   "raw html",
		TraceID:   "trace-raw",
		Timestamp: now,
		Metadata: map[string]any{
			"stage": "raw",
			"error": "minify failed",
		},
	}))
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://example.com/clean",
		Payload:   "clean html",
		TraceID:   "trace-clean",
		Timestamp: now,
	}))

	all, err := a.Scan(ctx, archiver.ScanOptions{})
	require.NoError(t, err)
	require.Len(t, all, 2)

	rawOnly, err := a.Scan(ctx, archiver.ScanOptions{Stage: "raw"})
	require.NoError(t, err)
	require.Len(t, rawOnly, 1)
	require.Equal(t, "trace-raw", rawOnly[0].TraceID)
	require.Equal(t, "minify failed", rawOnly[0].Error)
}

func TestLocalArchiver_Remove_SoftDelete(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	record := collector.Archive{
		URL:       "https://example.com/article",
		Payload:   "content",
		TraceID:   "trace-remove-me",
		Timestamp: time.Now(),
	}
	require.NoError(t, a.Save(ctx, record))

	_, err = a.Load(ctx, "trace-remove-me")
	require.NoError(t, err)

	// Remove is a soft delete — Load must return ErrNotFound afterwards.
	require.NoError(t, a.Remove(ctx, "trace-remove-me"))

	_, err = a.Load(ctx, "trace-remove-me")
	require.True(t, errors.Is(err, archiver.ErrNotFound))

	// But the .data file must still exist on disk.
	dataDir := filepath.Join(dir, "archives", time.Now().Format("2006/01/02"))
	dataFile := filepath.Join(dataDir, "trace-remove-me.data")
	_, statErr := os.Stat(dataFile)
	require.NoError(t, statErr, ".data file must remain after soft-delete")
}

func TestLocalArchiver_Remove_Idempotent(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://x.com",
		Payload:   "p",
		TraceID:   "trace-idem",
		Timestamp: time.Now(),
	}))
	require.NoError(t, a.Remove(ctx, "trace-idem"))
	// Second Remove must not error and must not overwrite the original deleted_at.
	require.NoError(t, a.Remove(ctx, "trace-idem"))
}

func TestLocalArchiver_Remove_NotFound(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	err = a.Remove(context.Background(), "ghost-trace")
	require.True(t, errors.Is(err, archiver.ErrNotFound))
}

func TestLocalArchiver_Scan_ExcludesRemovedByDefault(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://x.com/keep",
		Payload:   "keep",
		TraceID:   "trace-keep",
		Timestamp: now,
	}))
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://x.com/gone",
		Payload:   "gone",
		TraceID:   "trace-gone",
		Timestamp: now,
	}))
	require.NoError(t, a.Remove(ctx, "trace-gone"))

	// Default scan must hide soft-deleted entries.
	results, err := a.Scan(ctx, archiver.ScanOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "trace-keep", results[0].TraceID)

	// IncludeRemoved must expose both.
	all, err := a.Scan(ctx, archiver.ScanOptions{IncludeRemoved: true})
	require.NoError(t, err)
	require.Len(t, all, 2)

	// The soft-deleted entry must have DeletedAt set.
	var goneMeta *archiver.Meta
	for i := range all {
		if all[i].TraceID == "trace-gone" {
			goneMeta = &all[i]
		}
	}
	require.NotNil(t, goneMeta)
	require.NotNil(t, goneMeta.DeletedAt)
}

func TestLocalArchiver_Purge(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://x.com",
		Payload:   "p",
		TraceID:   "trace-purge",
		Timestamp: now,
	}))
	require.NoError(t, a.Remove(ctx, "trace-purge"))
	require.NoError(t, a.Purge(ctx, "trace-purge"))

	// Both files must be gone after Purge.
	dataDir := filepath.Join(dir, "archives", now.Format("2006/01/02"))
	_, err = os.Stat(filepath.Join(dataDir, "trace-purge.data"))
	require.True(t, os.IsNotExist(err), ".data must be deleted after Purge")
	_, err = os.Stat(filepath.Join(dataDir, "trace-purge.meta.json"))
	require.True(t, os.IsNotExist(err), ".meta.json must be deleted after Purge")
}

func TestLocalArchiver_Purge_RefusesActiveArchive(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://x.com",
		Payload:   "p",
		TraceID:   "trace-active",
		Timestamp: time.Now(),
	}))
	// Purge without prior Remove must fail.
	err = a.Purge(ctx, "trace-active")
	require.Error(t, err)
}

func TestLocalArchiver_PurgeAll(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()

	for _, id := range []string{"t1", "t2", "t3"} {
		require.NoError(t, a.Save(ctx, collector.Archive{
			URL: "https://x.com/" + id, Payload: id, TraceID: id, Timestamp: now,
		}))
	}
	// Soft-delete two of three.
	require.NoError(t, a.Remove(ctx, "t1"))
	require.NoError(t, a.Remove(ctx, "t2"))

	purged, err := a.PurgeAll(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, purged)

	// t3 must survive.
	_, err = a.Load(ctx, "t3")
	require.NoError(t, err)
}

func TestLocalArchiver_CreatedAt_Precision(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	// Use a time with sub-second precision to verify it survives the JSON round-trip.
	saved := time.Date(2026, 4, 16, 14, 52, 37, 123456789, time.UTC)
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL: "https://x.com", Payload: "p", TraceID: "trace-ts", Timestamp: saved,
	}))

	results, err := a.Scan(ctx, archiver.ScanOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	// CreatedAt must be second-precision at minimum (JSON RFC3339Nano preserves nanoseconds).
	require.WithinDuration(t, saved, results[0].CreatedAt, time.Second)
}

func TestLocalArchiver_Load_DetectsCorruption(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL: "https://x.com", Payload: "original content", TraceID: "trace-corrupt", Timestamp: now,
	}))

	// Tamper with the .data file directly.
	dataPath := filepath.Join(dir, "archives", now.Format("2006/01/02"), "trace-corrupt.data")
	require.NoError(t, os.WriteFile(dataPath, []byte("corrupted!"), 0644))

	_, err = a.Load(ctx, "trace-corrupt")
	require.True(t, errors.Is(err, archiver.ErrCorrupted), "expected ErrCorrupted, got: %v", err)
}

func TestLocalArchiver_Meta_HasPayloadSHA256(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://x.com",
		Payload:   "hello",
		TraceID:   "trace-sha",
		Timestamp: time.Now(),
	}))

	results, err := a.Scan(ctx, archiver.ScanOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NotEmpty(t, results[0].PayloadSHA256, "meta must record payload_sha256")
	require.Len(t, results[0].PayloadSHA256, 64, "SHA-256 hex string must be 64 characters")
}

func TestLocalArchiver_Load_ErrorOnMissingSHA256(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://x.com",
		Payload:   "legacy content",
		TraceID:   "trace-legacy",
		Timestamp: now,
	}))

	// Simulate a meta file that is missing payload_sha256 (e.g. manually written).
	metaPath := filepath.Join(dir, "archives", now.Format("2006/01/02"), "trace-legacy.meta.json")
	legacyMeta := []byte(`{"url":"https://x.com","trace_id":"trace-legacy"}`)
	require.NoError(t, os.WriteFile(metaPath, legacyMeta, 0644))

	// Load must refuse with ErrCorrupted — no payload_sha256 is not tolerated.
	_, err = a.Load(ctx, "trace-legacy")
	require.True(t, errors.Is(err, archiver.ErrCorrupted), "expected ErrCorrupted, got: %v", err)
}

// Verify that LocalArchiver satisfies both Archiver and collector.Saver.
var _ archiver.Archiver = (*archiver.LocalArchiver)(nil)
var _ collector.Saver = (*archiver.LocalArchiver)(nil)
