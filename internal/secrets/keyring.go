package secrets

import (
	"context"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// KeyringStore stores secrets in the user's operating-system keyring.
type KeyringStore struct {
	service string
	get     keyringGet
}

type keyringGet func(string, string) (string, error)

var _ SecretStore = (*KeyringStore)(nil)

// NewKeyringStore creates a store using service as the keyring service name.
func NewKeyringStore(service string) (*KeyringStore, error) {
	if strings.TrimSpace(service) == "" {
		return nil, fmt.Errorf("%w: service", ErrParamMissing)
	}
	return &KeyringStore{service: service, get: keyring.Get}, nil
}

// Get retrieves a secret from the operating-system keyring.
func (s *KeyringStore) Get(ctx context.Context, name string) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: store", ErrParamMissing)
	}
	if err := validateRequest(ctx, name); err != nil {
		return nil, err
	}
	if s.get == nil {
		return nil, fmt.Errorf("%w: keyring get", ErrParamMissing)
	}
	value, err := s.get(s.service, name)
	if err != nil {
		return nil, fmt.Errorf("get keyring secret %q: %w", name, err)
	}
	if value == "" {
		return nil, fmt.Errorf("keyring secret %q: %w", name, ErrEmptySecret)
	}
	return []byte(value), nil
}
