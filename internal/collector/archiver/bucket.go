package archiver

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// EnsureBucket creates bucket when it does not already exist.
func EnsureBucket(ctx context.Context, client *s3.Client, bucket string) error {
	if client == nil {
		return fmt.Errorf("%w: s3 client", ErrParamMissing)
	}
	if bucket == "" {
		return fmt.Errorf("%w: bucket", ErrParamMissing)
	}
	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}); err == nil {
		return nil
	}
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		if _, headErr := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}); headErr == nil {
			return nil
		}
		return fmt.Errorf("create s3 bucket %q: %w", bucket, err)
	}
	return nil
}
