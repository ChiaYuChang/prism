package prompt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Getter provides read access to stored prompt content.
type Getter interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// Resolver securely retrieves prompt objects by their content hash.
type Resolver struct {
	store  Getter
	verify bool
}

// NewResolver creates a prompt resolver. If verify is nil, it defaults to true.
// The store dependency must be provided.
func NewResolver(store Getter, verify *bool) (*Resolver, error) {
	if store == nil {
		return nil, errors.New("store dependency is required")
	}

	v := true
	if verify != nil {
		v = *verify
	}

	return &Resolver{
		store:  store,
		verify: v,
	}, nil
}

// Resolve reads prompt bytes corresponding to a content hash. If verification
// is enabled, it ensures the bytes match the requested hash.
func (r *Resolver) Resolve(ctx context.Context, hash string) ([]byte, error) {
	key := ObjectKey(hash)
	
	rc, err := r.store.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt from store: %w", err)
	}
	defer func() { _ = rc.Close() }()

	b, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("failed to read prompt content: %w", err)
	}

	if r.verify {
		expectedHash := strings.TrimPrefix(hash, "sha256:")
		hasher := sha256.New()
		hasher.Write(b)
		actualHash := hex.EncodeToString(hasher.Sum(nil))

		if actualHash != expectedHash {
			return nil, fmt.Errorf("prompt content hash mismatch: expected %s, got %s", expectedHash, actualHash)
		}
	}

	return b, nil
}
