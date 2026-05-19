package archiver_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	testBucket = "prism-archives"
)

var testEndpoint string

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start SeaweedFS container using testcontainers.
	swContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "chrislusf/seaweedfs:4.05",
			ExposedPorts: []string{"8333/tcp", "9333/tcp"},
			Cmd:          []string{"server", "-s3", "-s3.port=8333", "-dir=/data", "-ip.bind=0.0.0.0"},
			WaitingFor:   wait.ForHTTP("/cluster/status").WithPort("9333/tcp"),
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("failed to start seaweedfs container: %s", err))
	}

	// Try to get the host port mapped to the container's 8333.
	mappedPort, err := swContainer.MappedPort(ctx, "8333/tcp")
	if err != nil {
		panic(fmt.Sprintf("failed to get host port for 8333/tcp: %s", err))
	}

	testEndpoint = fmt.Sprintf("http://localhost:%s", mappedPort.Port())
	if err := waitForS3Endpoint(ctx, testEndpoint, 4); err != nil {
		panic(fmt.Sprintf("seaweedfs s3 endpoint not ready at %s: %s", testEndpoint, err))
	}

	// Create the test bucket before running tests.
	// SeaweedFS in test mode doesn't strictly check credentials unless configured.
	cfg, _ := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("any", "any", "")),
	)
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(testEndpoint)
		o.UsePathStyle = true
	})
	if err := createBucketWithRetry(ctx, client, testBucket, 4); err != nil {
		panic(fmt.Sprintf("failed to create test bucket %s: %s", testBucket, err))
	}

	// Run tests.
	code := m.Run()

	// Terminate container.
	if err := swContainer.Terminate(ctx); err != nil {
		panic(fmt.Errorf("failed to terminate seaweedfs container: %w", err))
	}

	os.Exit(code)
}

func waitForS3Endpoint(ctx context.Context, endpoint string, maxFailures int) error {
	client := &http.Client{Timeout: 2 * time.Second}
	return testutils.WithExponentialBackoff(ctx, maxFailures, 2*time.Second, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return nil
	})
}

func createBucketWithRetry(ctx context.Context, client *s3.Client, bucket string, maxFailures int) error {
	return testutils.WithExponentialBackoff(ctx, maxFailures, 2*time.Second, func() error {
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(bucket),
		})
		return err
	})
}

func newTestS3Client(t *testing.T) *s3.Client {
	t.Helper()
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("any", "any", "")),
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
			"kind":        "raw",
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
			"kind":  "raw",
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

	rawOnly, err := a.Scan(ctx, archiver.ScanOptions{PayloadKind: archiver.PayloadKindRaw})
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
			"kind":        "raw",
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
