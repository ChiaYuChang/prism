package infra

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/extra/redisprometheus/v9"
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

// ErrValkeyMetricsInitFailed indicates Valkey Prometheus metrics registration failed.
var ErrValkeyMetricsInitFailed = errors.New("valkey metrics init failed")

type ValkeyLocker struct {
	client    *redis.Client
	lockSha   string
	unlockSha string
}

type ValkeyClientConfig struct {
	Addr           string
	Username       string
	Password       string
	DB             int
	ClientName     string
	TracingEnabled bool
	MetricsEnabled bool
}

func (c ValkeyClientConfig) MetricsName() string {
	if c.ClientName != "" {
		return c.ClientName
	}
	return "default"
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

var (
	valkeyCollectorsMu sync.Mutex
	valkeyCollectors   = make(map[string]prometheus.Collector)
)

func registerValkeyMetrics(addr, name string, client *redis.Client) error {
	valkeyCollectorsMu.Lock()
	defer valkeyCollectorsMu.Unlock()

	key := addr + "/" + name
	if _, exists := valkeyCollectors[key]; exists {
		return fmt.Errorf("%w: duplicate collector addr=%s client=%s", ErrValkeyMetricsInitFailed, addr, name)
	}

	collector := redisprometheus.NewCollector("valkey", "client", client)
	labeledReg := prometheus.WrapRegistererWith(prometheus.Labels{"addr": addr, "client": name}, prometheus.DefaultRegisterer)
	if err := labeledReg.Register(collector); err != nil {
		return fmt.Errorf("%w: register valkey collector with prometheus: %w", ErrValkeyMetricsInitFailed, err)
	}
	valkeyCollectors[key] = collector
	return nil
}

// NewValkeyClient creates a new Redis client with optional tracing and metrics.
func NewValkeyClient(ctx context.Context, cfg ValkeyClientConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Username: cfg.Username,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping valkey: %w", err)
	}

	if cfg.TracingEnabled {
		if err := redisotel.InstrumentTracing(client); err != nil {
			return nil, fmt.Errorf("failed to instrument valkey tracing: %w", err)
		}
	}

	if cfg.MetricsEnabled {
		if err := registerValkeyMetrics(cfg.Addr, cfg.MetricsName(), client); err != nil {
			return nil, err
		}
	}

	return client, nil
}
