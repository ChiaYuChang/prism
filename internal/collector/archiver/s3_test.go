package archiver_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
)

const (
	testBucket   = "prism-archives"
	testEndpoint = "http://127.0.0.1:8333"
)

func newTestS3Client(t *testing.T) *s3.Client {
	t.Helper()
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(testEndpoint)
		o.UsePathStyle = true
	})
}

func newTestS3Archiver(t *testing.T, prefix string) *archiver.S3Archiver {
	t.Helper()
	client := newTestS3Client(t)
	a, err := archiver.NewS3Archiver(client, testBucket, prefix, testutils.Logger())
	require.NoError(t, err)
	return a
}

func TestS3Archiver_SaveAndLoad_RoundTrip(t *testing.T) {
	prefix := "test-" + t.Name()
	a := newTestS3Archiver(t, prefix)
	ctx := context.Background()

	record := collector.Archive{
		URL:       "https://example.com/s3-article",
		Payload:   "<html>s3 saved content</html>",
		TraceID:   "s3-trace-001",
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"stage":       "raw",
			"error":       "minify failed",
			"source_abbr": "dpp",
			"source_type": "PARTY",
			"batch_id":    "00000000-0000-0000-0000-000000000001",
		},
	}
	require.NoError(t, a.Save(ctx, record))

	got, err := a.Load(ctx, "s3-trace-001")
	require.NoError(t, err)
	require.Equal(t, "<html>s3 saved content</html>", got)
}

func TestS3Archiver_Load_NotFound(t *testing.T) {
	prefix := "test-" + t.Name()
	a := newTestS3Archiver(t, prefix)

	_, err := a.Load(context.Background(), "nonexistent-trace")
	require.Error(t, err)
	require.True(t, errors.Is(err, archiver.ErrNotFound))
}

func TestS3Archiver_Scan(t *testing.T) {
	prefix := "test-" + t.Name()
	a := newTestS3Archiver(t, prefix)
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://example.com/1",
		Payload:   "p1",
		TraceID:   "scan-t1",
		Timestamp: now,
		Metadata: map[string]any{
			"stage": "raw",
			"error": "fail1",
		},
	}))
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://example.com/2",
		Payload:   "p2",
		TraceID:   "scan-t2",
		Timestamp: now,
	}))

	all, err := a.Scan(ctx, archiver.ScanOptions{})
	require.NoError(t, err)
	require.Len(t, all, 2)

	rawOnly, err := a.Scan(ctx, archiver.ScanOptions{Stage: "raw"})
	require.NoError(t, err)
	require.Len(t, rawOnly, 1)
	require.Equal(t, "scan-t1", rawOnly[0].TraceID)
}

func TestS3Archiver_Scan_ByTraceID(t *testing.T) {
	prefix := "test-" + t.Name()
	a := newTestS3Archiver(t, prefix)
	ctx := context.Background()

	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://example.com/a",
		Payload:   "pa",
		TraceID:   "tid-a",
		Timestamp: time.Now(),
	}))
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://example.com/b",
		Payload:   "pb",
		TraceID:   "tid-b",
		Timestamp: time.Now(),
	}))

	results, err := a.Scan(ctx, archiver.ScanOptions{TraceID: "tid-a"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "tid-a", results[0].TraceID)
}

func TestS3Archiver_Remove_SoftDelete(t *testing.T) {
	prefix := "test-" + t.Name()
	a := newTestS3Archiver(t, prefix)
	ctx := context.Background()

	require.NoError(t, a.Save(ctx, collector.Archive{
		URL: "https://example.com/rm", Payload: "data", TraceID: "rm-trace", Timestamp: time.Now(),
	}))

	_, err := a.Load(ctx, "rm-trace")
	require.NoError(t, err)

	require.NoError(t, a.Remove(ctx, "rm-trace"))

	_, err = a.Load(ctx, "rm-trace")
	require.True(t, errors.Is(err, archiver.ErrNotFound))
}

func TestS3Archiver_Remove_Idempotent(t *testing.T) {
	prefix := "test-" + t.Name()
	a := newTestS3Archiver(t, prefix)
	ctx := context.Background()

	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://example.com/idem",
		Payload:   "d",
		TraceID:   "idem-trace",
		Timestamp: time.Now(),
	}))
	require.NoError(t, a.Remove(ctx, "idem-trace"))
	require.NoError(t, a.Remove(ctx, "idem-trace"))
}

func TestS3Archiver_Scan_ExcludesRemovedByDefault(t *testing.T) {
	prefix := "test-" + t.Name()
	a := newTestS3Archiver(t, prefix)
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://x.com/keep",
		Payload:   "k",
		TraceID:   "keep-t",
		Timestamp: now,
	}))
	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://x.com/gone",
		Payload:   "g",
		TraceID:   "gone-t",
		Timestamp: now,
	}))
	require.NoError(t, a.Remove(ctx, "gone-t"))

	results, err := a.Scan(ctx, archiver.ScanOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "keep-t", results[0].TraceID)

	all, err := a.Scan(ctx, archiver.ScanOptions{IncludeRemoved: true})
	require.NoError(t, err)
	require.Len(t, all, 2)
}

func TestS3Archiver_ScanMeta_SourceFields(t *testing.T) {
	prefix := "test-" + t.Name()
	a := newTestS3Archiver(t, prefix)
	ctx := context.Background()

	require.NoError(t, a.Save(ctx, collector.Archive{
		URL:       "https://example.com/src",
		Payload:   "p",
		TraceID:   "src-trace",
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"stage":       "raw",
			"source_abbr": "kmt",
			"source_type": "PARTY",
			"batch_id":    "batch-123",
		},
	}))

	results, err := a.Scan(ctx, archiver.ScanOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "kmt", results[0].SourceAbbr)
	require.Equal(t, "PARTY", results[0].SourceType)
	require.Equal(t, "batch-123", results[0].BatchID)
}
