package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
)

// StoreWithCache provides a generic decorator that adds caching to a persistent
// store. Persistent storage is authoritative, and the cache is a best-effort
// optimization layer.
type StoreWithCache struct {
	cache      Store
	persistent Store
}

// NewStoreWithCache creates a StoreWithCache. Both dependencies must be non-nil.
// If caching is not desired, callers should use the persistent store directly.
func NewStoreWithCache(cache Store, persistent Store) (*StoreWithCache, error) {
	if cache == nil {
		return nil, errors.New("cache store is required")
	}
	if persistent == nil {
		return nil, errors.New("persistent store is required")
	}
	return &StoreWithCache{
		cache:      cache,
		persistent: persistent,
	}, nil
}

var _ Store = (*StoreWithCache)(nil)

// Get attempts to read an object from the cache. On a miss, it reads from the
// persistent store. If the object is smaller than MaxObjectSize, it populates
// the cache on a best-effort basis.
func (s *StoreWithCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if cachedResult, err := s.cache.Get(ctx, key); err == nil {
		return cachedResult, nil
	}

	persistentResult, err := s.persistent.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	// Read up to MaxObjectSize + 1 to determine if the object is cacheable
	limitReader := io.LimitReader(persistentResult, MaxObjectSize+1)
	prefix, readErr := io.ReadAll(limitReader)
	if readErr != nil {
		_ = persistentResult.Close()
		return nil, readErr
	}

	if int64(len(prefix)) > MaxObjectSize {
		// Object exceeds cacheable size limit -> skip cache, return complete content
		return &multiReadCloser{
			reader: io.MultiReader(bytes.NewReader(prefix), persistentResult),
			closer: persistentResult,
		}, nil
	}

	// Content fits within limit -> cache it best-effort
	// We ignore Put errors since the persistent read succeeded.
	_ = s.cache.Put(ctx, key, bytes.NewReader(prefix), PutOptions{})

	// Return the buffered object to the caller
	_ = persistentResult.Close()
	return io.NopCloser(bytes.NewReader(prefix)), nil
}

// Put writes authoritative content to the persistent store and invalidates the cache.
func (s *StoreWithCache) Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error {
	if err := s.persistent.Put(ctx, key, body, opts); err != nil {
		return err
	}
	_ = s.cache.Delete(ctx, key)
	return nil
}

// List proxies directly to the persistent store. The cache is not authoritative.
func (s *StoreWithCache) List(ctx context.Context, prefix string) ([]Object, error) {
	return s.persistent.List(ctx, prefix)
}

// Delete removes an object from the persistent store and best-effort from cache.
func (s *StoreWithCache) Delete(ctx context.Context, key string) error {
	if err := s.persistent.Delete(ctx, key); err != nil {
		return err
	}
	_ = s.cache.Delete(ctx, key)
	return nil
}

// multiReadCloser combines an io.Reader (like io.MultiReader) with an io.Closer
// to satisfy io.ReadCloser.
type multiReadCloser struct {
	reader io.Reader
	closer io.Closer
}

func (m *multiReadCloser) Read(p []byte) (int, error) {
	return m.reader.Read(p)
}

func (m *multiReadCloser) Close() error {
	return m.closer.Close()
}
