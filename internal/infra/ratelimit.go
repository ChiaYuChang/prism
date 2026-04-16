package infra

import (
	"fmt"
	"os"
	"sync"

	"golang.org/x/time/rate"
	"gopkg.in/yaml.v3"
)

// RateLimiter controls dispatch rate per source abbreviation.
// The implementation is intentionally swappable: tests use NoOpRateLimiter,
// production uses InMemoryRateLimiter, and a future distributed implementation
// (e.g. Valkey-backed) can satisfy the same interface.
type RateLimiter interface {
	Allow(abbr string) bool
}

// LimiterSpec describes the token bucket parameters for one source.
type LimiterSpec struct {
	// Rate is the sustained token refill rate in tokens per second.
	Rate float64 `yaml:"rate"`
	// Burst is the bucket capacity (maximum instantaneous tokens).
	Burst int `yaml:"burst"`
}

// RateLimitConfig holds global defaults and per-source overrides.
type RateLimitConfig struct {
	// Defaults applies to any source not listed in Overrides.
	Defaults LimiterSpec `yaml:"defaults"`
	// Overrides maps source_abbr → LimiterSpec.
	Overrides map[string]LimiterSpec `yaml:"overrides"`
}

// DefaultRateLimitConfig returns a conservative baseline: 1 req/s, burst 2.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Defaults: LimiterSpec{Rate: 1.0, Burst: 2},
	}
}

// ReadRateLimitConfig loads a RateLimitConfig from a YAML file.
func ReadRateLimitConfig(path string) (RateLimitConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RateLimitConfig{}, fmt.Errorf("read rate limit config %s: %w", path, err)
	}
	var cfg RateLimitConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return RateLimitConfig{}, fmt.Errorf("parse rate limit config %s: %w", path, err)
	}
	if cfg.Defaults.Rate <= 0 {
		cfg.Defaults = DefaultRateLimitConfig().Defaults
	}
	if cfg.Defaults.Burst <= 0 {
		cfg.Defaults.Burst = 2
	}
	return cfg, nil
}

// InMemoryRateLimiter is a per-source token bucket rate limiter backed by
// golang.org/x/time/rate. Limiters are initialised lazily on first use.
// Since the scheduler holds a distributed lock ensuring only one instance
// dispatches at a time, in-memory state is sufficient.
type InMemoryRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	cfg      RateLimitConfig
}

// NewInMemoryRateLimiter creates a limiter registry from the given config.
func NewInMemoryRateLimiter(cfg RateLimitConfig) *InMemoryRateLimiter {
	return &InMemoryRateLimiter{
		mu:       sync.Mutex{},
		limiters: make(map[string]*rate.Limiter),
		cfg:      cfg,
	}
}

// Allow reports whether a token is available for the given source abbreviation
// and consumes it if so. Thread-safe.
func (r *InMemoryRateLimiter) Allow(abbr string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.limiters[abbr]
	if !ok {
		spec := r.cfg.Defaults
		if override, found := r.cfg.Overrides[abbr]; found {
			spec = override
		}
		l = rate.NewLimiter(rate.Limit(spec.Rate), spec.Burst)
		r.limiters[abbr] = l
	}
	return l.Allow()
}

// NoOpRateLimiter always allows every request. Suitable for tests and
// scheduler instances where rate limiting is not required.
type NoOpRateLimiter struct{}

func (NoOpRateLimiter) Allow(_ string) bool { return true }
