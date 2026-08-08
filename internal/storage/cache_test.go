package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

// mockStore is a simple map-backed Store for testing.
type mockStore struct {
	data        map[string][]byte
	getError    error
	putError    error
	deleteError error
	gets        int
	puts        int
	deletes     int
}

func newMockStore() *mockStore {
	return &mockStore{
		data: make(map[string][]byte),
	}
}

func (m *mockStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	m.gets++
	if m.getError != nil {
		return nil, m.getError
	}
	d, ok := m.data[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(d)), nil
}

func (m *mockStore) Stat(ctx context.Context, key string) (ObjectMetadata, error) {
	if m.getError != nil {
		return ObjectMetadata{}, m.getError
	}
	if v, ok := m.data[key]; ok {
		return ObjectMetadata{Key: key, Size: int64(len(v))}, nil
	}
	return ObjectMetadata{}, ErrNotFound
}

func (m *mockStore) Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error {
	m.puts++
	if m.putError != nil {
		return m.putError
	}
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.data[key] = b
	return nil
}

func (m *mockStore) Delete(ctx context.Context, key string) error {
	m.deletes++
	if m.deleteError != nil {
		return m.deleteError
	}
	if _, ok := m.data[key]; !ok {
		return ErrNotFound
	}
	delete(m.data, key)
	return nil
}

func (m *mockStore) List(ctx context.Context, prefix string) ([]Object, error) {
	var objs []Object
	for k, v := range m.data {
		objs = append(objs, Object{Key: k, Size: int64(len(v))})
	}
	return objs, nil
}

// testFailingReader returns an error upon reading.
type testFailingReader struct{}

func (t *testFailingReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("simulated read error")
}

func (t *testFailingReader) Close() error {
	return nil
}

// mockStoreWithReadError helps test read failures after successful Get.
type mockStoreWithReadError struct {
	mockStore
}

func (m *mockStoreWithReadError) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	m.gets++
	return &testFailingReader{}, nil
}

func TestStoreWithCache_Get_CacheHit(t *testing.T) {
	cache := newMockStore()
	persistent := newMockStore()
	store, _ := NewStoreWithCache(cache, persistent)

	cache.data["test-key"] = []byte("cached content")
	persistent.data["test-key"] = []byte("persistent content")

	rc, err := store.Get(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)

	if string(b) != "cached content" {
		t.Errorf("expected cached content, got %s", string(b))
	}
	if persistent.gets != 0 {
		t.Errorf("expected persistent.Get to not be called, got %d", persistent.gets)
	}
}

func TestStoreWithCache_Get_CacheMiss(t *testing.T) {
	cache := newMockStore()
	persistent := newMockStore()
	store, _ := NewStoreWithCache(cache, persistent)

	persistent.data["test-key"] = []byte("persistent content")

	rc, err := store.Get(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)

	if string(b) != "persistent content" {
		t.Errorf("expected persistent content, got %s", string(b))
	}
	if persistent.gets != 1 {
		t.Errorf("expected persistent.Get to be called")
	}
	if cache.puts != 1 {
		t.Errorf("expected cache to be populated")
	}
	if string(cache.data["test-key"]) != "persistent content" {
		t.Errorf("expected cache to contain content")
	}
}

func TestStoreWithCache_Get_CacheProviderError(t *testing.T) {
	cache := newMockStore()
	cache.getError = errors.New("cache offline")
	persistent := newMockStore()
	store, _ := NewStoreWithCache(cache, persistent)

	persistent.data["test-key"] = []byte("persistent content")

	rc, err := store.Get(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)

	if string(b) != "persistent content" {
		t.Errorf("expected fallback to persistent store")
	}
}

func TestStoreWithCache_Get_CachePopulateError(t *testing.T) {
	cache := newMockStore()
	cache.putError = errors.New("cache full")
	persistent := newMockStore()
	store, _ := NewStoreWithCache(cache, persistent)

	persistent.data["test-key"] = []byte("persistent content")

	rc, err := store.Get(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)

	if string(b) != "persistent content" {
		t.Errorf("expected persistent content despite cache put error")
	}
}

func TestStoreWithCache_Get_PersistentGetFailure(t *testing.T) {
	cache := newMockStore()
	persistent := newMockStore()
	persistent.getError = ErrNotFound
	store, _ := NewStoreWithCache(cache, persistent)

	_, err := store.Get(context.Background(), "test-key")
	if err != ErrNotFound {
		t.Errorf("expected persistent error to propagate, got %v", err)
	}
}

func TestStoreWithCache_Get_PersistentReadFailure(t *testing.T) {
	cache := newMockStore()
	persistent := &mockStoreWithReadError{}
	store, _ := NewStoreWithCache(cache, persistent)

	_, err := store.Get(context.Background(), "test-key")
	if err == nil {
		t.Errorf("expected persistent read error to propagate")
	}
}

func TestStoreWithCache_Put_Success(t *testing.T) {
	cache := newMockStore()
	persistent := newMockStore()
	store, _ := NewStoreWithCache(cache, persistent)

	cache.data["test-key"] = []byte("old cache")
	err := store.Put(context.Background(), "test-key", bytes.NewReader([]byte("new content")), PutOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if persistent.puts != 1 {
		t.Errorf("expected persistent.Put to be called")
	}
	if string(persistent.data["test-key"]) != "new content" {
		t.Errorf("expected persistent to have new content")
	}
	if cache.deletes != 1 {
		t.Errorf("expected cache to be invalidated")
	}
	if _, exists := cache.data["test-key"]; exists {
		t.Errorf("expected cache key to be deleted")
	}
}

func TestStoreWithCache_Put_PersistentFailure(t *testing.T) {
	cache := newMockStore()
	persistent := newMockStore()
	persistent.putError = errors.New("s3 offline")
	store, _ := NewStoreWithCache(cache, persistent)

	cache.data["test-key"] = []byte("old cache")
	err := store.Put(context.Background(), "test-key", bytes.NewReader([]byte("new content")), PutOptions{})
	if err == nil {
		t.Errorf("expected persistent put error to propagate")
	}
	if cache.deletes > 0 {
		t.Errorf("expected cache to NOT be invalidated when persistent put fails")
	}
}

func TestStoreWithCache_Put_CacheInvalidationFailure(t *testing.T) {
	cache := newMockStore()
	cache.deleteError = errors.New("cache unreachable")
	persistent := newMockStore()
	store, _ := NewStoreWithCache(cache, persistent)

	err := store.Put(context.Background(), "test-key", bytes.NewReader([]byte("new content")), PutOptions{})
	if err != nil {
		t.Fatalf("expected success despite cache deletion failure, got: %v", err)
	}
}

func TestStoreWithCache_Delete_Success(t *testing.T) {
	cache := newMockStore()
	persistent := newMockStore()
	store, _ := NewStoreWithCache(cache, persistent)

	persistent.data["test-key"] = []byte("val")
	cache.data["test-key"] = []byte("val")

	err := store.Delete(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, exists := persistent.data["test-key"]; exists {
		t.Errorf("expected persistent to be deleted")
	}
	if _, exists := cache.data["test-key"]; exists {
		t.Errorf("expected cache to be deleted")
	}
}

func TestStoreWithCache_List(t *testing.T) {
	cache := newMockStore()
	persistent := newMockStore()
	store, _ := NewStoreWithCache(cache, persistent)

	persistent.data["k1"] = []byte("v1")
	persistent.data["k2"] = []byte("v2")
	cache.data["k3"] = []byte("v3")

	objs, err := store.List(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(objs) != 2 {
		t.Errorf("expected exactly 2 items from persistent, got %d", len(objs))
	}
}

func TestStoreWithCache_Get_ObjectTooLarge(t *testing.T) {
	cache := newMockStore()
	persistent := newMockStore()
	store, _ := NewStoreWithCache(cache, persistent)

	// Create an object exactly 1 byte larger than MaxObjectSize
	largeContent := make([]byte, MaxObjectSize+1)
	for i := range largeContent {
		largeContent[i] = 'a'
	}
	persistent.data["large-key"] = largeContent

	rc, err := store.Get(context.Background(), "large-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = rc.Close() }()

	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}

	if len(b) != int(MaxObjectSize)+1 {
		t.Errorf("expected complete large content length %d, got %d", MaxObjectSize+1, len(b))
	}
	if cache.puts > 0 {
		t.Errorf("expected object not to be cached due to size limit")
	}
}

func TestStoreWithCache_Get_CacheReaderFailure(t *testing.T) {
	cache := &mockStoreWithReadError{mockStore: *newMockStore()}
	// simulate Cache.Get succeeding but reader failing
	cache.data["test-key"] = []byte("this will not be read successfully")
	
	persistent := newMockStore()
	persistent.data["test-key"] = []byte("persistent content")
	
	store, _ := NewStoreWithCache(cache, persistent)
	
	rc, err := store.Get(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)
	
	if string(b) != "persistent content" {
		t.Errorf("expected persistent content after cache read failure, got %s", string(b))
	}
	if persistent.gets != 1 {
		t.Errorf("expected persistent.Get to be called, got %d", persistent.gets)
	}
}
