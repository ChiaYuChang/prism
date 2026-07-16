package factory_test

import (
	"context"
	"testing"

	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/ChiaYuChang/prism/internal/storage/factory"
	"github.com/stretchr/testify/require"
)

func TestNewFileStore(t *testing.T) {
	store, err := factory.New(context.Background(), storage.URI{Scheme: "file", Root: t.TempDir()}, nil)
	require.NoError(t, err)
	require.NotNil(t, store)
}

func TestNewS3StoreRequiresClient(t *testing.T) {
	_, err := factory.New(context.Background(), storage.URI{Scheme: "s3", Bucket: "bucket"}, nil)
	require.Error(t, err)
}

func TestNewSFTPStoreIsNotImplemented(t *testing.T) {
	_, err := factory.New(context.Background(), storage.URI{Scheme: "sftp", Host: "host", Prefix: "path"}, nil)
	require.Error(t, err)
}
