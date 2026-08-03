package objectstore_test

import (
	"testing"

	"github.com/ChiaYuChang/prism/internal/storage/objectstore"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
)

func TestNewS3StoreRequiresClient(t *testing.T) {
	_, err := objectstore.NewS3Store(nil, "bucket", "prefix")
	require.Error(t, err)
}

func TestNewS3StoreRequiresBucket(t *testing.T) {
	_, err := objectstore.NewS3Store(s3.NewFromConfig(aws.Config{}), "", "prefix")
	require.Error(t, err)
}
