package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/require"
)

func TestParseCLI_StatusSubcommand(t *testing.T) {
	var buf bytes.Buffer
	opts, err := parseCLI([]string{"status", "--archive", "/tmp/test"}, &buf)
	require.NoError(t, err)
	require.Equal(t, "status", opts.subcommand)
	require.Equal(t, "/tmp/test", opts.archiveURI)
}

func TestParseCLI_RunWithAllFlags(t *testing.T) {
	var buf bytes.Buffer
	opts, err := parseCLI([]string{
		"run",
		"--archive", "./data/archives",
		"--since", "2026-04-01",
		"--until", "2026-04-15",
		"--limit", "10",
		"--trace-id", "abc123",
		"--dry-run",
		"--pg-host", "db.local",
		"--pg-port", "5433",
		"--pg-db", "testdb",
	}, &buf)
	require.NoError(t, err)
	require.Equal(t, "run", opts.subcommand)
	require.Equal(t, "./data/archives", opts.archiveURI)
	require.Equal(t, "2026-04-01", opts.since.Format("2006-01-02"))
	require.Equal(t, "2026-04-15", opts.until.Format("2006-01-02"))
	require.Equal(t, 10, opts.limit)
	require.Equal(t, "abc123", opts.traceID)
	require.True(t, opts.dryRun)
	require.Equal(t, "db.local", opts.postgres.Host)
	require.Equal(t, 5433, opts.postgres.Port)
	require.Equal(t, "testdb", opts.postgres.DB)
}

func TestParseCLI_MissingArchive(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseCLI([]string{"status"}, &buf)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUsage)
}

func TestParseCLI_UnknownSubcommand(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseCLI([]string{"unknown"}, &buf)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUsage)
}

func TestParseCLI_NoArgs(t *testing.T) {
	var buf bytes.Buffer
	_, err := parseCLI(nil, &buf)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUsage)
}

func TestParseCLI_CleanWithPurge(t *testing.T) {
	var buf bytes.Buffer
	opts, err := parseCLI([]string{"clean", "--archive", "/tmp/a", "--purge"}, &buf)
	require.NoError(t, err)
	require.Equal(t, "clean", opts.subcommand)
	require.True(t, opts.purge)
}

func TestRunStatus(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, a.Save(ctx, collector.Archive{
		URL: "https://example.com/1", Payload: "p1", TraceID: "t1", Timestamp: now,
		Metadata: map[string]any{"stage": "raw", "error": "minify failed"},
	}))
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL: "https://example.com/2", Payload: "p2", TraceID: "t2", Timestamp: now,
		Metadata: map[string]any{"stage": "raw", "error": "parse error"},
	}))
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL: "https://example.com/3", Payload: "p3", TraceID: "t3", Timestamp: now,
	}))

	err = runStatus(ctx, a, cliOptions{})
	require.NoError(t, err)
}

func TestRunList(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, a.Save(ctx, collector.Archive{
		URL: "https://example.com/article", Payload: "html", TraceID: "trace-abc", Timestamp: now,
		Metadata: map[string]any{"stage": "raw", "error": "minify: unexpected EOF", "source_abbr": "dpp"},
	}))

	err = runList(ctx, a, cliOptions{})
	require.NoError(t, err)
}

func TestRunList_Empty(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	err = runList(context.Background(), a, cliOptions{})
	require.NoError(t, err)
}

func TestRunList_WithFilters(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.NewLocalArchiver(dir, testutils.Logger())
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, a.Save(ctx, collector.Archive{
		URL: "https://example.com/1", Payload: "p", TraceID: "t1", Timestamp: now,
	}))
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL: "https://example.com/2", Payload: "p", TraceID: "t2", Timestamp: now,
	}))

	err = runList(ctx, a, cliOptions{limit: 1})
	require.NoError(t, err)
}
