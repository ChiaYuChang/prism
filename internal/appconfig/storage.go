package appconfig

import (
	"context"
	"fmt"

	"github.com/ChiaYuChang/prism/internal/storage"
	storagefactory "github.com/ChiaYuChang/prism/internal/storage/factory"
)

// NewStorage creates a configured storage backend from a URI and the shared
// S3 connection settings.
func NewStorage(ctx context.Context, uri string, s3cfg S3Config) (storage.Store, error) {
	spec, err := storage.ParseURI(uri)
	if err != nil {
		return nil, err
	}
	if spec.Scheme == "s3" {
		s3Client, err := s3cfg.NewClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("build storage s3 client: %w", err)
		}
		return storagefactory.New(ctx, spec, s3Client)
	}
	return storagefactory.New(ctx, spec, nil)
}
