package archiver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Archiver stores archives in an S3-compatible bucket (AWS S3, SeaweedFS, MinIO).
//
// Object layout mirrors LocalArchiver:
//
//	{prefix}/archives/{YYYY}/{MM}/{DD}/{traceID}.data       payload body
//	{prefix}/archives/{YYYY}/{MM}/{DD}/{traceID}.meta.json  metadata body (JSON)
//
// Soft-delete stamps deleted_at in the .meta.json object (same as Local).
// Hard-delete should use S3 lifecycle policies, not application code.
type S3Archiver struct {
	client *s3.Client
	bucket string
	prefix string
	logger *slog.Logger
}

var _ Archiver = (*S3Archiver)(nil)
var _ collector.Saver = (*S3Archiver)(nil)

func NewS3Archiver(client *s3.Client, bucket, prefix string, logger *slog.Logger) (*S3Archiver, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: s3 client", ErrParamMissing)
	}
	if bucket == "" {
		return nil, fmt.Errorf("%w: bucket", ErrParamMissing)
	}
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	return &S3Archiver{
		client: client,
		bucket: bucket,
		prefix: strings.TrimRight(prefix, "/"),
		logger: logger,
	}, nil
}

func (a *S3Archiver) Save(ctx context.Context, record collector.Archive) error {
	dateStr := record.Timestamp.Format("2006/01/02")
	base := "archives/" + dateStr + "/" + record.TraceID
	dataKey := a.key(base + ".data")
	metaKey := a.key(base + ".meta.json")

	_, err := a.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(dataKey),
		Body:   strings.NewReader(record.Payload),
	})
	if err != nil {
		return fmt.Errorf("s3 put data %s/%s: %w", a.bucket, dataKey, err)
	}

	metaBytes, err := buildMetaJSON(record)
	if err != nil {
		return fmt.Errorf("marshal metadata for %s: %w", record.TraceID, err)
	}

	_, err = a.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(a.bucket),
		Key:         aws.String(metaKey),
		Body:        bytes.NewReader(metaBytes),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("s3 put meta %s/%s: %w", a.bucket, metaKey, err)
	}

	a.logger.DebugContext(ctx, "archived to s3",
		slog.String("bucket", a.bucket),
		slog.String("key", dataKey),
	)
	return nil
}

func (a *S3Archiver) Load(ctx context.Context, traceID string) (string, error) {
	dataKey, err := a.findKey(ctx, traceID, ".data")
	if err != nil {
		return "", err
	}

	metaKey := strings.TrimSuffix(dataKey, ".data") + ".meta.json"
	m, metaErr := a.readMeta(ctx, metaKey)
	if metaErr == nil && m.DeletedAt != nil {
		return "", fmt.Errorf("%w: trace_id=%s (soft-deleted at %s)",
			ErrNotFound, traceID, m.DeletedAt.Format(time.RFC3339))
	}

	resp, err := a.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(dataKey),
	})
	if err != nil {
		return "", fmt.Errorf("s3 get %s/%s: %w", a.bucket, dataKey, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read s3 object %s: %w", dataKey, err)
	}

	// Integrity check: meta must carry payload_sha256; mismatch or absence → ErrCorrupted.
	if metaErr == nil {
		if m.PayloadSHA256 == "" {
			a.logger.ErrorContext(ctx, "archive meta is missing payload_sha256",
				slog.String("trace_id", traceID),
				slog.String("meta_key", metaKey),
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

	a.logger.DebugContext(ctx, "loaded archive from s3",
		slog.String("trace_id", traceID),
		slog.String("key", dataKey),
		slog.Int("bytes", len(data)),
	)
	return string(data), nil
}

func (a *S3Archiver) Scan(ctx context.Context, opts ScanOptions) ([]Meta, error) {
	prefix := a.key("archives/")

	paginator := s3.NewListObjectsV2Paginator(a.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(a.bucket),
		Prefix: aws.String(prefix),
	})

	var results []Meta
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list s3 objects in %s: %w", a.bucket, err)
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if !strings.HasSuffix(key, ".meta.json") {
				continue
			}
			if opts.TraceID != "" && !strings.HasSuffix(key, "/"+opts.TraceID+".meta.json") {
				continue
			}

			m, err := a.readMeta(ctx, key)
			if err != nil {
				a.logger.WarnContext(ctx, "skipping unreadable meta", slog.String("key", key), slog.Any("error", err))
				continue
			}
			if !matchesScanOpts(m, opts) {
				continue
			}
			results = append(results, m)
			if opts.Limit > 0 && len(results) >= opts.Limit {
				goto done
			}
		}
	}
done:
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.Before(results[j].CreatedAt)
	})
	return results, nil
}

// Remove soft-deletes by stamping deleted_at in the .meta.json object.
func (a *S3Archiver) Remove(ctx context.Context, traceID string) error {
	metaKey, err := a.findKey(ctx, traceID, ".meta.json")
	if err != nil {
		return err
	}

	resp, err := a.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(metaKey),
	})
	if err != nil {
		return fmt.Errorf("s3 get meta %s: %w", metaKey, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read meta %s: %w", metaKey, err)
	}

	updated, alreadyDeleted, err := stampDeletedAtInJSON(data, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("stamp deleted_at in %s: %w", metaKey, err)
	}
	if alreadyDeleted {
		return nil
	}

	_, err = a.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(a.bucket),
		Key:         aws.String(metaKey),
		Body:        bytes.NewReader(updated),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("s3 put meta %s: %w", metaKey, err)
	}

	a.logger.DebugContext(ctx, "soft-removed archive in s3",
		slog.String("trace_id", traceID),
		slog.String("key", metaKey),
	)
	return nil
}

// ----- helpers -----

func (a *S3Archiver) key(suffix string) string {
	if a.prefix == "" {
		return suffix
	}
	return a.prefix + "/" + suffix
}

func (a *S3Archiver) findKey(ctx context.Context, traceID, suffix string) (string, error) {
	needle := "/" + traceID + suffix
	prefix := a.key("archives/")

	paginator := s3.NewListObjectsV2Paginator(a.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(a.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("list s3 objects: %w", err)
		}
		for _, obj := range page.Contents {
			if strings.HasSuffix(aws.ToString(obj.Key), needle) {
				return aws.ToString(obj.Key), nil
			}
		}
	}
	return "", fmt.Errorf("%w: trace_id=%s", ErrNotFound, traceID)
}

func (a *S3Archiver) readMeta(ctx context.Context, metaKey string) (Meta, error) {
	resp, err := a.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(metaKey),
	})
	if err != nil {
		return Meta{}, fmt.Errorf("s3 get meta %s: %w", metaKey, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Meta{}, fmt.Errorf("read meta %s: %w", metaKey, err)
	}

	fallback := parseDateFromKeyPath(metaKey)
	m, err := parseMeta(data, fallback)
	if err != nil {
		return Meta{}, fmt.Errorf("unmarshal meta %s: %w", metaKey, err)
	}
	return m, nil
}
