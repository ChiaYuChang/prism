// Package factory constructs storage backends from parsed storage URIs.
package factory

import (
	"context"
	"fmt"

	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/ChiaYuChang/prism/internal/storage/filesystem"
	"github.com/ChiaYuChang/prism/internal/storage/objectstore"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// New creates a storage backend for spec. An S3 client is required only for
// s3 URIs; it is ignored for filesystem URIs.
func New(ctx context.Context, spec storage.URI, s3Client *s3.Client) (storage.Store, error) {
	switch spec.Scheme {
	case "file":
		return filesystem.NewLocalStore(spec.Root)
	case "s3":
		if s3Client == nil {
			return nil, fmt.Errorf("s3 client is required for s3 storage")
		}
		if err := objectstore.EnsureBucket(ctx, s3Client, spec.Bucket); err != nil {
			return nil, err
		}
		return objectstore.NewS3Store(s3Client, spec.Bucket, spec.Prefix)
	case "sftp":
		return nil, fmt.Errorf("sftp storage is not implemented")
	default:
		return nil, fmt.Errorf("unsupported storage scheme %q", spec.Scheme)
	}
}
