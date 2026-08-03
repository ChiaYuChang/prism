package infra

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ChiaYuChang/prism/internal/tree"
	"github.com/redis/go-redis/v9"
)

const SchedulerToggleGlobalName = "global"
const SchedulerToggleGlobalKey = "scheduler.toggle"

// SchedulerToggleStore persists global and per-scheduler enable switches.
type SchedulerToggleStore struct {
	client *redis.Client
}

// SchedulerToggleState describes a scheduler node and its effective state.
type SchedulerToggleState struct {
	Name             string
	Enabled          bool
	EffectiveEnabled bool
	Parent           string
}

func NewSchedulerToggleStore(client *redis.Client) (*SchedulerToggleStore, error) {
	if client == nil {
		return nil, fmt.Errorf("param missing: valkey client")
	}
	return &SchedulerToggleStore{client: client}, nil
}

func SchedulerToggleKey(name string) string {
	return "scheduler.toggle." + name
}

// Initialize preserves normal startup state and forces a paused startup when requested.
func (s *SchedulerToggleStore) Initialize(ctx context.Context, name string, enabled bool) error {
	value := strconv.FormatBool(enabled)
	var err error
	if enabled {
		err = s.client.SetNX(ctx, SchedulerToggleKey(name), value, 0).Err()
	} else {
		err = s.client.Set(ctx, SchedulerToggleKey(name), value, 0).Err()
	}
	if err != nil {
		return fmt.Errorf("initialize scheduler toggle %s: %w", name, err)
	}
	if err := s.client.SetNX(ctx, SchedulerToggleGlobalKey, "true", 0).Err(); err != nil {
		return fmt.Errorf("initialize global scheduler toggle: %w", err)
	}
	return nil
}

// Enabled returns the effective state: global AND per-scheduler.
func (s *SchedulerToggleStore) Enabled(ctx context.Context, name string) (bool, error) {
	state, err := s.State(ctx, name)
	if err != nil {
		return false, err
	}
	return state.EffectiveEnabled, nil
}

func (s *SchedulerToggleStore) GlobalEnabled(ctx context.Context) (bool, error) {
	value, err := s.client.Get(ctx, SchedulerToggleGlobalKey).Result()
	if err == redis.Nil {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read global scheduler toggle: %w", err)
	}
	return toggleValue(value, true)
}

func (s *SchedulerToggleStore) SetGlobal(ctx context.Context, enabled bool) error {
	return s.set(ctx, SchedulerToggleGlobalKey, enabled)
}

func (s *SchedulerToggleStore) SetScheduler(ctx context.Context, name string, enabled bool) error {
	return s.set(ctx, SchedulerToggleKey(name), enabled)
}

// State returns a scheduler node's local and effective enablement.
func (s *SchedulerToggleStore) State(ctx context.Context, name string) (SchedulerToggleState, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return SchedulerToggleState{}, fmt.Errorf("scheduler name is required")
	}
	values, err := s.client.MGet(ctx, SchedulerToggleGlobalKey, SchedulerToggleKey(name)).Result()
	if err != nil {
		return SchedulerToggleState{}, fmt.Errorf("read scheduler toggles: %w", err)
	}
	global, err := toggleValue(values[0], true)
	if err != nil {
		return SchedulerToggleState{}, fmt.Errorf("parse global scheduler toggle: %w", err)
	}
	local, err := toggleValue(values[1], true)
	if err != nil {
		return SchedulerToggleState{}, fmt.Errorf("parse scheduler toggle %s: %w", name, err)
	}

	toggles := tree.New[bool]()
	if err := toggles.Add(SchedulerToggleGlobalName, "", global); err != nil {
		return SchedulerToggleState{}, fmt.Errorf("build scheduler toggle tree: %w", err)
	}
	if err := toggles.Add(name, SchedulerToggleGlobalName, local); err != nil {
		return SchedulerToggleState{}, fmt.Errorf("build scheduler toggle tree: %w", err)
	}
	effective, err := toggles.Effective(name, func(enabled bool) bool { return enabled })
	if err != nil {
		return SchedulerToggleState{}, fmt.Errorf("evaluate scheduler toggle tree: %w", err)
	}
	return SchedulerToggleState{
		Name:             name,
		Enabled:          local,
		EffectiveEnabled: effective,
		Parent:           SchedulerToggleGlobalName,
	}, nil
}

func (s *SchedulerToggleStore) set(ctx context.Context, key string, enabled bool) error {
	if err := s.client.Set(ctx, key, strconv.FormatBool(enabled), 0).Err(); err != nil {
		return fmt.Errorf("set scheduler toggle %s: %w", key, err)
	}
	return nil
}

func toggleValue(value any, defaultValue bool) (bool, error) {
	if value == nil {
		return defaultValue, nil
	}
	s, ok := value.(string)
	if !ok {
		return false, fmt.Errorf("unexpected value type %T", value)
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(s))
	if err != nil {
		return false, err
	}
	return parsed, nil
}
