// Package objectstore implements storage.Store using an S3-compatible API.
package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// S3Store stores objects in AWS S3 or an S3-compatible service such as
// SeaweedFS or MinIO.
type S3Store struct {
	client *s3.Client
	bucket string
	prefix string
}

var _ storage.Store = (*S3Store)(nil)

// NewS3Store creates an S3-backed store. The client is configured by the
// caller, allowing AWS S3, SeaweedFS, and MinIO to share this implementation.
func NewS3Store(client *s3.Client, bucket, prefix string) (*S3Store, error) {
	if client == nil {
		return nil, fmt.Errorf("s3 client is required")
	}
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("s3 bucket is required")
	}
	cleanPrefix, err := storage.NormalizeKey(prefix, true)
	if err != nil {
		return nil, err
	}
	return &S3Store{client: client, bucket: bucket, prefix: cleanPrefix}, nil
}

// Put writes an object to S3.
func (s *S3Store) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) error {
	key, err := storage.NormalizeKey(key, false)
	if err != nil {
		return err
	}
	if body == nil {
		return fmt.Errorf("storage object body is nil")
	}
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(key)),
		Body:   body,
	}
	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}
	if _, err := s.client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("put s3 object %s/%s: %w", s.bucket, key, err)
	}
	return nil
}

// Get retrieves an object from S3.
func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	key, err := storage.NormalizeKey(key, false)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(key)),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound") {
			return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
		}
		return nil, fmt.Errorf("get s3 object %s/%s: %w", s.bucket, key, err)
	}
	return resp.Body, nil
}

// List returns objects below prefix in the store namespace.
func (s *S3Store) List(ctx context.Context, prefix string) ([]storage.Object, error) {
	prefix, err := storage.NormalizeKey(prefix, true)
	if err != nil {
		return nil, err
	}
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.fullKey(prefix)),
	})
	objects := make([]storage.Object, 0)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list s3 objects below %s: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			key := strings.TrimPrefix(aws.ToString(obj.Key), s.fullKey(""))
			objects = append(objects, storage.Object{Key: key, Size: aws.ToInt64(obj.Size)})
		}
	}
	return objects, nil
}

// Delete removes an object from S3.
func (s *S3Store) Delete(ctx context.Context, key string) error {
	key, err := storage.NormalizeKey(key, false)
	if err != nil {
		return err
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(key)),
	}); err != nil {
		return fmt.Errorf("delete s3 object %s/%s: %w", s.bucket, key, err)
	}
	return nil
}

// EnsureBucket creates bucket when it does not already exist.
func EnsureBucket(ctx context.Context, client *s3.Client, bucket string) error {
	if client == nil {
		return fmt.Errorf("s3 client is required")
	}
	if strings.TrimSpace(bucket) == "" {
		return fmt.Errorf("s3 bucket is required")
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

func (s *S3Store) fullKey(key string) string {
	if s.prefix == "" {
		return key
	}
	if key == "" {
		return s.prefix + "/"
	}
	return s.prefix + "/" + key
}
