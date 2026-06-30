package auth

import (
	"context"
	"fmt"
	"time"

	authtoken "github.com/ChiaYuChang/prism/internal/auth/token"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

type TokenStore interface {
	GetTokenByID(ctx context.Context, id uuid.UUID) (repo.Token, error)
	GetRootToken(ctx context.Context) (repo.Token, error)
}

type Principal struct {
	TokenID uuid.UUID
	Type    authtoken.Type
}

type Authenticator struct {
	store        TokenStore
	allowedTypes map[authtoken.Type]struct{}
	now          func() time.Time
}

type AuthenticatorParams struct {
	Store        TokenStore
	AllowedTypes []authtoken.Type
}

func NewAuthenticator(params AuthenticatorParams) (*Authenticator, error) {
	if params.Store == nil {
		return nil, fmt.Errorf("%w: store", ErrParamMissing)
	}
	allowed := make(map[authtoken.Type]struct{}, len(params.AllowedTypes))
	for _, typ := range params.AllowedTypes {
		allowed[typ] = struct{}{}
	}
	return &Authenticator{store: params.Store, allowedTypes: allowed, now: time.Now}, nil
}

func (a *Authenticator) AuthenticateToken(ctx context.Context, raw string) (Principal, error) {
	parsed, err := authtoken.Parse(raw)
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	if !a.allows(parsed.Type) {
		return Principal{}, ErrForbidden
	}
	row, err := a.store.GetTokenByID(ctx, parsed.ID)
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	if row.Type != string(parsed.Type) {
		return Principal{}, ErrInvalidToken
	}
	if row.RevokedAt != nil {
		return Principal{}, ErrTokenRevoked
	}
	if !a.now().Before(row.ExpiresAt) {
		return Principal{}, ErrTokenExpired
	}
	hasher, err := authtoken.NewHasher(row.HashAlgorithm)
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	if !hasher.Verify(parsed.Secret, row.TokenHash) {
		return Principal{}, ErrUnauthorized
	}
	return Principal{TokenID: row.ID, Type: parsed.Type}, nil
}

func (a *Authenticator) AuthenticateRoot(ctx context.Context, raw string) (Principal, error) {
	if !a.allows(authtoken.TypeRoot) {
		return Principal{}, ErrForbidden
	}
	secret, err := NormalizeRootSecret(raw)
	if err != nil {
		return Principal{}, err
	}
	row, err := a.store.GetRootToken(ctx)
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	if row.Type != string(authtoken.TypeRoot) {
		return Principal{}, ErrInvalidToken
	}
	if row.RevokedAt != nil {
		return Principal{}, ErrTokenRevoked
	}
	hasher, err := authtoken.NewHasher(row.HashAlgorithm)
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	if !hasher.Verify([]byte(secret), row.TokenHash) {
		return Principal{}, ErrUnauthorized
	}
	return Principal{TokenID: row.ID, Type: authtoken.TypeRoot}, nil
}

func (a *Authenticator) allows(typ authtoken.Type) bool {
	if len(a.allowedTypes) == 0 {
		return true
	}
	_, ok := a.allowedTypes[typ]
	return ok
}
