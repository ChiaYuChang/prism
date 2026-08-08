package prompt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/ChiaYuChang/prism/internal/storage"
)

type mockGetter struct {
	data     map[string][]byte
	err      error
	getCalls int
}

func (m *mockGetter) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	m.getCalls++
	if m.err != nil {
		return nil, m.err
	}
	if b, ok := m.data[key]; ok {
		return io.NopCloser(bytes.NewReader(b)), nil
	}
	return nil, storage.ErrNotFound
}

type failReadGetter struct{}

func (m *failReadGetter) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return &failingReader{}, nil
}

type failingReader struct{}

func (f *failingReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("simulated read error")
}

func (f *failingReader) Close() error {
	return nil
}

func TestNewResolver(t *testing.T) {
	store := &mockGetter{}

	_, err := NewResolver(nil, nil)
	if err == nil {
		t.Error("expected error for nil store")
	}

	r, err := NewResolver(store, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.verify != true {
		t.Error("expected default verify to be true")
	}

	v := false
	r, _ = NewResolver(store, &v)
	if r.verify != false {
		t.Error("expected verify to be false")
	}

	v = true
	r, _ = NewResolver(store, &v)
	if r.verify != true {
		t.Error("expected verify to be true")
	}
}

func TestResolver_Resolve_Success(t *testing.T) {
	content := []byte("hello world")
	hasher := sha256.New()
	hasher.Write(content)
	hash := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	key := ObjectKey(hash)

	store := &mockGetter{
		data: map[string][]byte{
			key: content,
		},
	}
	r, _ := NewResolver(store, nil)

	b, err := r.Resolve(context.Background(), hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(b) != "hello world" {
		t.Errorf("expected %s, got %s", "hello world", string(b))
	}
}

func TestResolver_Resolve_HashMismatchFail(t *testing.T) {
	content := []byte("hello world")
	hash := "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef" // fake hash
	key := ObjectKey(hash)

	store := &mockGetter{
		data: map[string][]byte{
			key: content,
		},
	}
	v := true
	r, _ := NewResolver(store, &v)

	_, err := r.Resolve(context.Background(), hash)
	if err == nil {
		t.Fatal("expected hash mismatch error")
	}
}

func TestResolver_Resolve_HashMismatchSkipVerify(t *testing.T) {
	content := []byte("hello world")
	hash := "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef" // fake hash
	key := ObjectKey(hash)

	store := &mockGetter{
		data: map[string][]byte{
			key: content,
		},
	}
	v := false
	r, _ := NewResolver(store, &v)

	b, err := r.Resolve(context.Background(), hash)
	if err != nil {
		t.Fatalf("unexpected error when verification is disabled: %v", err)
	}
	if string(b) != "hello world" {
		t.Errorf("expected %s, got %s", "hello world", string(b))
	}
}

func TestResolver_Resolve_StoreError(t *testing.T) {
	store := &mockGetter{
		err: errors.New("s3 unreachable"),
	}
	r, _ := NewResolver(store, nil)

	_, err := r.Resolve(context.Background(), "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	if err == nil {
		t.Fatal("expected store get error to propagate")
	}
}

func TestResolver_Resolve_ReadError(t *testing.T) {
	store := &failReadGetter{}
	r, _ := NewResolver(store, nil)

	_, err := r.Resolve(context.Background(), "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	if err == nil {
		t.Fatal("expected read error to propagate")
	}
}

func TestResolver_Resolve_InvalidHashFormat(t *testing.T) {
	store := &mockGetter{}
	r, _ := NewResolver(store, nil)

	_, err := r.Resolve(context.Background(), "invalid-hash-format")
	if err == nil {
		t.Fatal("expected invalid hash format error")
	}
	if store.getCalls != 0 {
		t.Fatalf("expected 0 Get calls, got %d", store.getCalls)
	}
}

func TestResolver_Resolve_ExactMaxObjectSize(t *testing.T) {
	content := make([]byte, storage.MaxObjectSize)
	hasher := sha256.New()
	hasher.Write(content)
	hash := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	key := ObjectKey(hash)

	store := &mockGetter{
		data: map[string][]byte{
			key: content,
		},
	}
	v := false
	r, _ := NewResolver(store, &v)

	b, err := r.Resolve(context.Background(), hash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(b) != int(storage.MaxObjectSize) {
		t.Fatalf("expected length %d, got %d", storage.MaxObjectSize, len(b))
	}
}

func TestResolver_Resolve_ExceedsMaxObjectSize(t *testing.T) {
	content := make([]byte, storage.MaxObjectSize+1)
	hasher := sha256.New()
	hasher.Write(content)
	hash := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	key := ObjectKey(hash)

	store := &mockGetter{
		data: map[string][]byte{
			key: content,
		},
	}
	v := false
	r, _ := NewResolver(store, &v)

	_, err := r.Resolve(context.Background(), hash)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, storage.ErrObjectTooLarge) {
		t.Fatalf("expected %v, got %v", storage.ErrObjectTooLarge, err)
	}
}
