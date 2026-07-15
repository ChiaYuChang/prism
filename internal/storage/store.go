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

// PutOptions controls optional metadata written with an object.
type PutOptions struct {
	ContentType string
}

// Object describes an object returned by List.
type Object struct {
	Key  string
	Size int64
}

// Store provides durable object and file storage.
//
// Keys use slash-separated paths regardless of the backing storage. The
// caller owns key naming and namespace separation.
type Store interface {
	Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]Object, error)
	Delete(ctx context.Context, key string) error
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
