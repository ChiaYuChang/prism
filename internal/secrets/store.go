// Package secrets provides a small abstraction over local secret stores.
package secrets

import (
	"context"
	"errors"
)

// ErrParamMissing indicates that a required store parameter was not supplied.
var ErrParamMissing = errors.New("param missing")

// ErrEmptySecret indicates that a provider returned an empty secret.
var ErrEmptySecret = errors.New("empty secret")

// SecretStore reads named secrets.
type SecretStore interface {
	Get(ctx context.Context, name string) ([]byte, error)
}
