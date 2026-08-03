package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type rbwCommand func(context.Context, ...string) ([]byte, error)

// RBWStore reads secrets from an unlocked rbw vault.
// Secret values should be managed in rbw itself.
type RBWStore struct {
	command rbwCommand
}

var _ SecretStore = (*RBWStore)(nil)

// NewRBWStore creates a store backed by the rbw executable on PATH.
func NewRBWStore() *RBWStore {
	return &RBWStore{command: runRBW}
}

// Get retrieves the password field for an rbw entry by name, URI, or UUID.
func (s *RBWStore) Get(ctx context.Context, name string) ([]byte, error) {
	if err := validateRequest(ctx, name); err != nil {
		return nil, err
	}
	if s == nil || s.command == nil {
		return nil, fmt.Errorf("%w: command", ErrParamMissing)
	}
	out, err := s.command(ctx, "get", "--raw", name)
	if err != nil {
		return nil, fmt.Errorf("get rbw secret %q: %w", name, err)
	}
	value, err := parseRBWPassword(out)
	if err != nil {
		return nil, fmt.Errorf("parse rbw secret %q: %w", name, err)
	}
	if value == "" {
		return nil, fmt.Errorf("rbw secret %q: %w", name, ErrEmptySecret)
	}
	return []byte(value), nil
}

func runRBW(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "rbw", args...).Output()
}

func parseRBWPassword(raw []byte) (string, error) {
	var item struct {
		Password string `json:"password"`
		Data     struct {
			Password string `json:"password"`
		} `json:"data"`
		Login struct {
			Password string `json:"password"`
		} `json:"login"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return "", err
	}
	if item.Password != "" {
		return strings.TrimSuffix(item.Password, "\n"), nil
	}
	if item.Data.Password != "" {
		return strings.TrimSuffix(item.Data.Password, "\n"), nil
	}
	return strings.TrimSuffix(item.Login.Password, "\n"), nil
}

func validateRequest(ctx context.Context, name string) error {
	if ctx == nil {
		return fmt.Errorf("%w: context", ErrParamMissing)
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: name", ErrParamMissing)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
