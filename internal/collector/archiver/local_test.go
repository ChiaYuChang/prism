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

func TestLocalArchiver_Remove(t *testing.T) {
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

	require.NoError(t, a.Remove(ctx, "trace-remove-me"))

	_, err = a.Load(ctx, "trace-remove-me")
	require.True(t, errors.Is(err, archiver.ErrNotFound))
}

func TestLocalArchiver_Remove_NotFound(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	err = a.Remove(context.Background(), "ghost-trace")
	require.True(t, errors.Is(err, archiver.ErrNotFound))
}

// Verify that LocalArchiver satisfies both Archiver and collector.Saver.
var _ archiver.Archiver = (*archiver.LocalArchiver)(nil)
var _ collector.Saver = (*archiver.LocalArchiver)(nil)
