package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const progressCacheKeyPrefix = "fetch:progress:"

// ValkeyProgressCache stores FetchProgressResponse bodies in Valkey/Redis.
//
// Two TTLs are used: LiveTTL for non-terminal responses (so in-flight progress
// stays fresh) and TerminalTTL for terminal responses (cheap to keep around;
// once terminal=true the body is immutable).
type ValkeyProgressCache struct {
	Client      *redis.Client
	LiveTTL     time.Duration
	TerminalTTL time.Duration
}

// NewValkeyProgressCache validates parameters and returns a ready cache.
func NewValkeyProgressCache(client *redis.Client, liveTTL, terminalTTL time.Duration) (*ValkeyProgressCache, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: client", ErrParamMissing)
	}
	if liveTTL <= 0 {
		return nil, fmt.Errorf("liveTTL must be > 0")
	}
	if terminalTTL <= 0 {
		return nil, fmt.Errorf("terminalTTL must be > 0")
	}
	return &ValkeyProgressCache{Client: client, LiveTTL: liveTTL, TerminalTTL: terminalTTL}, nil
}

func progressCacheKey(id uuid.UUID) string {
	return progressCacheKeyPrefix + id.String()
}

func (c *ValkeyProgressCache) Get(ctx context.Context, fetchID uuid.UUID) (FetchProgressResponse, bool, error) {
	raw, err := c.Client.Get(ctx, progressCacheKey(fetchID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return FetchProgressResponse{}, false, nil
		}
		return FetchProgressResponse{}, false, fmt.Errorf("valkey get: %w", err)
	}
	var resp FetchProgressResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return FetchProgressResponse{}, false, fmt.Errorf("valkey decode: %w", err)
	}
	return resp, true, nil
}

func (c *ValkeyProgressCache) Set(ctx context.Context, fetchID uuid.UUID, resp FetchProgressResponse) error {
	body, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("valkey encode: %w", err)
	}
	ttl := c.LiveTTL
	if resp.Terminal {
		ttl = c.TerminalTTL
	}
	if err := c.Client.Set(ctx, progressCacheKey(fetchID), body, ttl).Err(); err != nil {
		return fmt.Errorf("valkey set: %w", err)
	}
	return nil
}
