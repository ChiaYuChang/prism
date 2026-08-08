// Package objectstore implements storage.Store using an S3-compatible API.
package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// S3Store stores objects in AWS S3 or an S3-compatible service such as
// SeaweedFS or MinIO.
type S3Store struct {
	client *s3.Client
	bucket string
	prefix string
}

var _ storage.ImmutableStore = (*S3Store)(nil)

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
		return providerError("put", key, err)
	}
	return nil
}

// PutIfAbsent writes an object only when key does not already exist. An
// existing object is accepted only when its complete content matches body.
func (s *S3Store) PutIfAbsent(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.PutIfAbsentResult, error) {
	cleanKey, err := storage.NormalizeKey(key, false)
	if err != nil {
		return storage.PutIfAbsentResult{}, err
	}
	if body == nil {
		return storage.PutIfAbsentResult{}, fmt.Errorf("storage object body is nil")
	}
	data, err := readBounded(body)
	if err != nil {
		return storage.PutIfAbsentResult{}, err
	}
	size := int64(len(data))
	digest := fmt.Sprintf("%x", storage.Hash(data))
	wantedMeta := storage.ObjectMetadata{Key: cleanKey, Size: size, ContentType: opts.ContentType}
	wantedChecksum := storage.ObjectChecksum{Size: size, SHA256: digest}
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(s.fullKey(cleanKey)),
		Body:        bytes.NewReader(data),
		IfNoneMatch: aws.String("*"),
		Metadata:    map[string]string{"sha256": digest},
	}
	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}
	if _, err := s.client.PutObject(ctx, input); err == nil {
		return storage.PutIfAbsentResult{Created: true, Metadata: wantedMeta, Checksum: wantedChecksum}, nil
	} else if !isPreconditionFailure(err) {
		return storage.PutIfAbsentResult{}, providerError("put", cleanKey, err)
	}

	existingMeta, statErr := s.Stat(ctx, cleanKey)
	if statErr != nil {
		return storage.PutIfAbsentResult{}, providerError("verify existing object after conditional write (stat)", cleanKey, statErr)
	}
	existingChecksum, chkErr := s.Checksum(ctx, cleanKey)
	if chkErr != nil {
		return storage.PutIfAbsentResult{}, providerError("verify existing object after conditional write (checksum)", cleanKey, chkErr)
	}
	
	if existingChecksum.Size == wantedChecksum.Size && existingChecksum.SHA256 == wantedChecksum.SHA256 {
		return storage.PutIfAbsentResult{Metadata: existingMeta, Checksum: existingChecksum}, nil
	}
	return storage.PutIfAbsentResult{Metadata: existingMeta, Checksum: existingChecksum}, fmt.Errorf("%w: %s", storage.ErrContentMismatch, cleanKey)
}

// Stat returns cheap metadata for key without reading the complete object.
func (s *S3Store) Stat(ctx context.Context, key string) (storage.ObjectMetadata, error) {
	cleanKey, err := storage.NormalizeKey(key, false)
	if err != nil {
		return storage.ObjectMetadata{}, err
	}
	resp, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(cleanKey)),
	})
	if err != nil {
		if isNotFound(err) {
			return storage.ObjectMetadata{}, fmt.Errorf("%w: %s", storage.ErrNotFound, cleanKey)
		}
		return storage.ObjectMetadata{}, providerError("stat", cleanKey, err)
	}
	return storage.ObjectMetadata{
		Key:         cleanKey,
		Size:        aws.ToInt64(resp.ContentLength),
		ContentType: aws.ToString(resp.ContentType),
	}, nil
}

// Checksum returns bounded, content-verified metadata for key. It does not use an
// ETag as a content hash, because ETags are not reliable for multipart objects.
func (s *S3Store) Checksum(ctx context.Context, key string) (storage.ObjectChecksum, error) {
	cleanKey, err := storage.NormalizeKey(key, false)
	if err != nil {
		return storage.ObjectChecksum{}, err
	}
	resp, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(cleanKey)),
	})
	if err != nil {
		if isNotFound(err) {
			return storage.ObjectChecksum{}, fmt.Errorf("%w: %s", storage.ErrNotFound, cleanKey)
		}
		return storage.ObjectChecksum{}, providerError("stat for checksum", cleanKey, err)
	}
	size := aws.ToInt64(resp.ContentLength)
	if size > storage.MaxObjectSize {
		return storage.ObjectChecksum{Size: size}, fmt.Errorf("%w: %s", storage.ErrObjectTooLarge, cleanKey)
	}
	object, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(cleanKey)),
	})
	if err != nil {
		if isNotFound(err) {
			return storage.ObjectChecksum{}, fmt.Errorf("%w: %s", storage.ErrNotFound, cleanKey)
		}
		return storage.ObjectChecksum{}, providerError("read for checksum", cleanKey, err)
	}
	digest, actualSize, hashErr := storage.HashReader(object.Body, storage.MaxObjectSize)
	closeErr := object.Body.Close()
	if hashErr != nil {
		return storage.ObjectChecksum{}, providerError("hash", cleanKey, hashErr)
	}
	if closeErr != nil {
		return storage.ObjectChecksum{}, providerError("close", cleanKey, closeErr)
	}
	if actualSize != size {
		return storage.ObjectChecksum{}, fmt.Errorf("%w: object %s changed during checksum", storage.ErrProvider, cleanKey)
	}
	return storage.ObjectChecksum{Size: size, SHA256: fmt.Sprintf("%x", digest)}, nil
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
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
		}
		return nil, providerError("get", key, err)
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
			return nil, providerError("list", prefix, err)
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
		return providerError("delete", key, err)
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

func readBounded(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, storage.MaxObjectSize+1))
	if err != nil {
		return nil, fmt.Errorf("read storage object body: %w", err)
	}
	if int64(len(data)) > storage.MaxObjectSize {
		return nil, storage.ErrObjectTooLarge
	}
	return data, nil
}

// readBounded and hashBounded replaced by centralized storage.HashReader

func providerError(operation, key string, err error) error {
	return fmt.Errorf("%w: %s s3 object %s: %w", storage.ErrProvider, operation, key, err)
}

func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound") {
		return true
	}
	var responseErr *smithyhttp.ResponseError
	return errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == http.StatusNotFound
}

func isPreconditionFailure(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "PreconditionFailed" {
		return true
	}
	var responseErr *smithyhttp.ResponseError
	return errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == http.StatusPreconditionFailed
}
