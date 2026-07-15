package archiver

import (
	"fmt"
	"log/slog"

	"github.com/ChiaYuChang/prism/internal/storage/objectstore"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Archiver stores archives using an S3-compatible storage store.
type S3Archiver struct {
	*archiveStore
}

var _ Archiver = (*S3Archiver)(nil)

// NewS3Archiver creates an S3Archiver for AWS S3, SeaweedFS, or MinIO.
func NewS3Archiver(client *s3.Client, bucket, prefix string, logger *slog.Logger) (*S3Archiver, error) {
	store, err := objectstore.NewS3Store(client, bucket, prefix)
	if err != nil {
		return nil, fmt.Errorf("create s3 archive store: %w", err)
	}
	archive, err := newArchiveStore(store, logger)
	if err != nil {
		return nil, err
	}
	return &S3Archiver{archiveStore: archive}, nil
}
