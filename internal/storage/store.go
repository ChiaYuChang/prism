// Package storage defines the backend-neutral interface for durable files and
// blobs used by Prism.
package storage

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"
)

// ErrNotFound indicates that a requested storage object does not exist.
var ErrNotFound = errors.New("storage object not found")

// ErrContentMismatch indicates that PutIfAbsent found an object at the key,
// but its content differs from the requested content.
var ErrContentMismatch = errors.New("storage object content mismatch")

// ErrObjectTooLarge indicates that an operation requiring an exact content
// hash exceeded MaxObjectSize.
var ErrObjectTooLarge = errors.New("storage object is too large")

// ErrProvider indicates a storage backend or provider failure. The underlying
// provider error is wrapped when one is available.
var ErrProvider = errors.New("storage provider error")

// MaxObjectSize is the largest object for which the immutable storage
// operations read and verify the complete content.
const MaxObjectSize int64 = 1 << 20

// PutOptions controls optional metadata written with an object.
type PutOptions struct {
	ContentType string
}

// Object describes an object returned by List.
type Object struct {
	Key  string
	Size int64
}

// ObjectMetadata describes an object returned by Stat or PutIfAbsent.
type ObjectMetadata struct {
	Key         string
	Size        int64
	ContentType string
}

// ObjectChecksum describes an object's full-content identity and size returned by Checksum.
type ObjectChecksum struct {
	Size   int64
	SHA256 string
}

// PutIfAbsentResult describes the outcome of PutIfAbsent. Created is false
// when an existing object has matching content. On ErrContentMismatch,
// Metadata and Checksum describe the existing object when it could be verified.
type PutIfAbsentResult struct {
	Created  bool
	Metadata ObjectMetadata
	Checksum ObjectChecksum
}

// Store provides durable object and file storage.
//
// Keys use slash-separated paths regardless of the backing storage. The
// caller owns key naming and namespace separation.
type Store interface {
	Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Stat(ctx context.Context, key string) (ObjectMetadata, error)
	List(ctx context.Context, prefix string) ([]Object, error)
	Delete(ctx context.Context, key string) error
}

// ImmutableStore is the optional report-artifact capability implemented by
// backends that support bounded, content-verified, no-overwrite writes.
// Existing Store implementations only need these methods when they opt into
// this capability.
type ImmutableStore interface {
	Store
	PutIfAbsent(ctx context.Context, key string, body io.Reader, opts PutOptions) (PutIfAbsentResult, error)
	Checksum(ctx context.Context, key string) (ObjectChecksum, error)
}

// NormalizeKey validates and normalizes a slash-separated storage key.
// Absolute keys and parent traversal are rejected so filesystem-backed stores
// cannot escape their configured root.
func NormalizeKey(key string, allowEmpty bool) (string, error) {
	key = strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
	if key == "" && allowEmpty {
		return "", nil
	}
	if key == "" || strings.HasPrefix(key, "/") {
		return "", errors.New("invalid storage key")
	}
	clean := path.Clean(key)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("invalid storage key")
	}
	return clean, nil
}
