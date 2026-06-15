package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/redis/go-redis/v9"
)

const defaultStatusMonitorKey = "api:status"

// ValkeyMonitor stores service health snapshots in Valkey/Redis.
type ValkeyMonitor struct {
	Client *redis.Client
	Key    string

	mu   sync.RWMutex
	mode string
}

// NewValkeyMonitor validates parameters and returns a Valkey-backed monitor.
func NewValkeyMonitor(client *redis.Client, mode, key string) (*ValkeyMonitor, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: client", ErrParamMissing)
	}
	if key == "" {
		key = defaultStatusMonitorKey
	}
	return &ValkeyMonitor{Client: client, Key: key, mode: mode}, nil
}

func (m *ValkeyMonitor) Mode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mode
}

func (m *ValkeyMonitor) SetMode(mode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mode = mode
}

func (m *ValkeyMonitor) InitializeStatuses(ctx context.Context, services []string) error {
	if len(services) == 0 {
		return nil
	}
	pipe := m.Client.Pipeline()
	now := time.Now()
	for _, service := range services {
		status := obs.HealthStatus{
			Level:     obs.LevelStarting,
			Message:   "Waiting for first heartbeat",
			Timestamp: now,
		}
		body, err := json.Marshal(status)
		if err != nil {
			return fmt.Errorf("valkey monitor encode: %w", err)
		}
		pipe.HSetNX(ctx, m.Key, service, body)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("valkey monitor initialize: %w", err)
	}
	return nil
}

func (m *ValkeyMonitor) SetStatus(ctx context.Context, service string, status obs.HealthStatus) error {
	body, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("valkey monitor encode: %w", err)
	}
	if err := m.Client.HSet(ctx, m.Key, service, body).Err(); err != nil {
		return fmt.Errorf("valkey monitor set: %w", err)
	}
	return nil
}

func (m *ValkeyMonitor) Statuses(ctx context.Context) (map[string]obs.HealthStatus, error) {
	raw, err := m.Client.HGetAll(ctx, m.Key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return map[string]obs.HealthStatus{}, nil
		}
		return nil, fmt.Errorf("valkey monitor get: %w", err)
	}
	statuses := make(map[string]obs.HealthStatus, len(raw))
	for service, body := range raw {
		var status obs.HealthStatus
		if err := json.Unmarshal([]byte(body), &status); err != nil {
			return nil, fmt.Errorf("valkey monitor decode %s: %w", service, err)
		}
		statuses[service] = status
	}
	return statuses, nil
}
