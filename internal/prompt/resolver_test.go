package prompt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

type mockGetter struct {
	data map[string][]byte
	err  error
}

func (m *mockGetter) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.err != nil {
		return nil, m.err
	}
	if b, ok := m.data[key]; ok {
		return io.NopCloser(bytes.NewReader(b)), nil
	}
	return nil, errors.New("not found")
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
	hash := hex.EncodeToString(hasher.Sum(nil))
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
	hash := "1234567890abcdef" // fake hash
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
	hash := "1234567890abcdef" // fake hash
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

	_, err := r.Resolve(context.Background(), "anyhash")
	if err == nil {
		t.Fatal("expected store get error to propagate")
	}
}

func TestResolver_Resolve_ReadError(t *testing.T) {
	store := &failReadGetter{}
	r, _ := NewResolver(store, nil)

	_, err := r.Resolve(context.Background(), "anyhash")
	if err == nil {
		t.Fatal("expected read error to propagate")
	}
}
