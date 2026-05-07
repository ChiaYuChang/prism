package api

import (
	"context"

	"github.com/google/uuid"
)

// ProgressCache caches FetchProgressResponse bodies keyed by fetch_id.
//
// Implementations must be safe for concurrent use. Get returns ok=false on
// cache miss with a nil error; transport errors are returned with ok=false.
// Set MUST pick the TTL based on the terminal flag: short live-TTL for
// in-flight fetches, long terminal-TTL for completed ones.
type ProgressCache interface {
	Get(ctx context.Context, fetchID uuid.UUID) (resp FetchProgressResponse, ok bool, err error)
	Set(ctx context.Context, fetchID uuid.UUID, resp FetchProgressResponse) error
}

// NoOpProgressCache disables caching. Get always misses, Set is a no-op.
type NoOpProgressCache struct{}

func (NoOpProgressCache) Get(context.Context, uuid.UUID) (FetchProgressResponse, bool, error) {
	return FetchProgressResponse{}, false, nil
}

func (NoOpProgressCache) Set(context.Context, uuid.UUID, FetchProgressResponse) error {
	return nil
}
