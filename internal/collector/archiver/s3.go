package archiver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Archiver stores archives in an S3-compatible bucket.
//
// Object layout:
//
//	archives/{YYYY}/{MM}/{DD}/{traceID}.data       payload
//	archives/{YYYY}/{MM}/{DD}/{traceID}.meta.json  metadata (via object metadata)
//
// Load, Scan, and Remove are not yet implemented (stubs that return ErrNotFound).
type S3Archiver struct {
	client *s3.Client
	bucket string
	prefix string // optional key prefix, no trailing slash
	logger *slog.Logger
}

var _ Archiver = (*S3Archiver)(nil)
var _ collector.Saver = (*S3Archiver)(nil)

// NewS3Archiver constructs an S3Archiver targeting bucket under optional prefix.
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

// Save uploads the payload to S3. Metadata fields are stored as S3 object
// metadata headers.
func (a *S3Archiver) Save(ctx context.Context, record collector.Archive) error {
	dateStr := record.Timestamp.Format("2006/01/02")
	key := a.key("archives/" + dateStr + "/" + record.TraceID + ".data")

	s3Meta := map[string]string{
		"url":      record.URL,
		"trace_id": record.TraceID,
	}
	if record.Fingerprint != "" {
		s3Meta["fingerprint"] = record.Fingerprint
	}
	if len(record.Metadata) > 0 {
		if b, err := json.Marshal(record.Metadata); err == nil {
			s3Meta["prism_metadata"] = string(b)
		}
	}

	_, err := a.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:   aws.String(a.bucket),
		Key:      aws.String(key),
		Body:     strings.NewReader(record.Payload),
		Metadata: s3Meta,
	})
	if err != nil {
		return fmt.Errorf("s3 put %s/%s: %w", a.bucket, key, err)
	}

	a.logger.DebugContext(ctx, "archived to s3",
		slog.String("bucket", a.bucket),
		slog.String("key", key),
	)
	return nil
}

// Load is not yet implemented for S3.
func (a *S3Archiver) Load(_ context.Context, traceID string) (string, error) {
	return "", fmt.Errorf("s3 Load not implemented: %w", ErrNotFound)
}

// Scan is not yet implemented for S3.
func (a *S3Archiver) Scan(_ context.Context, _ ScanOptions) ([]Meta, error) {
	return nil, fmt.Errorf("s3 Scan not implemented")
}

// Remove is not yet implemented for S3.
func (a *S3Archiver) Remove(_ context.Context, traceID string) error {
	return fmt.Errorf("s3 Remove not implemented: %w", ErrNotFound)
}

func (a *S3Archiver) key(suffix string) string {
	if a.prefix == "" {
		return suffix
	}
	return a.prefix + "/" + suffix
}
