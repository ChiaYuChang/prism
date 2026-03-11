package infra

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// Lua script to acquire a lock safely.
	// Returns 1 if acquired, 0 otherwise.
	lockLua = `
if redis.call("SET", KEYS[1], ARGV[1], "NX", "PX", ARGV[2]) then
    return 1
else
    return 0
end
`
	// Lua script to release a lock safely.
	// Only deletes if the value matches to prevent accidental unlocking of others.
	unlockLua = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end
`
)

type ValkeyLocker struct {
	client    *redis.Client
	lockSha   string
	unlockSha string
}

// NewValkeyLocker creates a locker and pre-loads Lua scripts for performance.
func NewValkeyLocker(ctx context.Context, client *redis.Client) (*ValkeyLocker, error) {
	lockSha, err := client.ScriptLoad(ctx, lockLua).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to load lock script: %w", err)
	}

	unlockSha, err := client.ScriptLoad(ctx, unlockLua).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to load unlock script: %w", err)
	}

	return &ValkeyLocker{
		client:    client,
		lockSha:   lockSha,
		unlockSha: unlockSha,
	}, nil
}

// TryLock attempts to acquire a lock with a unique ID and TTL.
// Returns a unique value (secret) if successful, otherwise empty string.
func (v *ValkeyLocker) TryLock(ctx context.Context, key string, ttl time.Duration) (string, error) {
	// Generate a unique secret for this specific lock attempt
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(b)

	res, err := vClientEvalSha(ctx, v.client, v.lockSha, []string{key}, secret, int(ttl.Milliseconds()))
	if err != nil {
		return "", err
	}

	if res == int64(1) {
		return secret, nil
	}
	return "", nil
}

// Unlock releases the lock only if the secret matches.
func (v *ValkeyLocker) Unlock(ctx context.Context, key, secret string) error {
	_, err := vClientEvalSha(ctx, v.client, v.unlockSha, []string{key}, secret)
	return err
}

// Helper to handle EvalSha results
func vClientEvalSha(ctx context.Context, client *redis.Client, sha string, keys []string, args ...interface{}) (interface{}, error) {
	return client.EvalSha(ctx, sha, keys, args...).Result()
}

// NewValkeyClient (Previous helper)
func NewValkeyClient(ctx context.Context, opts *redis.Options) (*redis.Client, error) {
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping valkey: %w", err)
	}
	return client, nil
}
