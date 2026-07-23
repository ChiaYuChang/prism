package auth

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"
	"unicode"

	"github.com/ChiaYuChang/prism/internal/auth/permission"
	authtoken "github.com/ChiaYuChang/prism/internal/auth/token"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrParamMissing     = errors.New("param missing")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrForbidden        = errors.New("forbidden")
	ErrRootAlreadyExist = errors.New("active root token already exists")
	ErrInvalidToken     = errors.New("invalid token")
	ErrTokenExpired     = errors.New("token expired")
	ErrTokenRevoked     = errors.New("token revoked")
)

const (
	DefaultRootTokenMinLength = 64
	RootTokenName             = "root"
)

type Actor struct {
	TokenID     uuid.UUID
	Type        authtoken.Type
	Name        string
	Permissions permission.Permission
}

type TokenTypeConfig struct {
	DefaultTTL time.Duration
	MaxTTL     time.Duration
}

type Service struct {
	tokens     repo.Tokens
	hasher     authtoken.Hasher
	auth       *Authenticator
	rootAuth   *Authenticator
	tokenTypes map[authtoken.Type]TokenTypeConfig
	now        func() time.Time
}

type ServiceParams struct {
	Tokens     repo.Tokens
	Hasher     authtoken.Hasher
	TokenTypes map[authtoken.Type]TokenTypeConfig
}

type CreateTokenRequest struct {
	Type        authtoken.Type
	Name        string
	Permissions *permission.Permission
	ExpiresAt   *time.Time
}

type TokenSecretResult struct {
	Token repo.Token
	Raw   string
}

func NewService(params ServiceParams) (*Service, error) {
	if params.Tokens == nil {
		return nil, fmt.Errorf("%w: tokens", ErrParamMissing)
	}
	if params.Hasher == nil {
		return nil, fmt.Errorf("%w: hasher", ErrParamMissing)
	}
	tokenTypes := make(
		map[authtoken.Type]TokenTypeConfig,
		len(params.TokenTypes),
	)
	maps.Copy(tokenTypes, params.TokenTypes)

	authenticator, err := NewAuthenticator(
		AuthenticatorParams{
			Store: params.Tokens,
			AllowedTypes: []authtoken.Type{
				authtoken.TypeAdmin,
				authtoken.TypeUser,
			},
		},
	)
	if err != nil {
		return nil, err
	}
	rootAuthenticator, err := NewAuthenticator(
		AuthenticatorParams{
			Store:        params.Tokens,
			AllowedTypes: []authtoken.Type{authtoken.TypeRoot},
		},
	)
	if err != nil {
		return nil, err
	}
	return &Service{
		tokens:     params.Tokens,
		hasher:     params.Hasher,
		auth:       authenticator,
		rootAuth:   rootAuthenticator,
		tokenTypes: tokenTypes,
		now:        time.Now,
	}, nil
}

func (s *Service) AuthenticateToken(ctx context.Context, raw string) (Actor, error) {
	principal, err := s.auth.AuthenticateToken(ctx, raw)
	return Actor{TokenID: principal.TokenID, Type: principal.Type, Name: principal.Name, Permissions: principal.Permissions}, err
}

func (s *Service) AuthenticateRoot(ctx context.Context, raw string) error {
	_, err := s.rootAuth.AuthenticateRoot(ctx, raw)
	return err
}

func (s *Service) InitRoot(ctx context.Context, raw string) (repo.Token, error) {
	secret, err := NormalizeRootSecret(raw)
	if err != nil {
		return repo.Token{}, err
	}
	if _, err := s.tokens.GetRootToken(ctx); err == nil {
		return repo.Token{}, ErrRootAlreadyExist
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return repo.Token{}, err
	}
	created, err := s.tokens.CreateToken(ctx,
		repo.CreateTokenParams{
			ID:            uuid.Must(uuid.NewV7()),
			Type:          string(authtoken.TypeRoot),
			Name:          RootTokenName,
			Permissions:   uint8(permission.Root),
			HashAlgorithm: s.hasher.Algorithm(),
			TokenHash:     s.hasher.Hash([]byte(secret)),
			ExpiresAt:     farFuture(),
		})
	if err != nil {
		return repo.Token{}, mapCreateTokenError(err)
	}
	return created, nil
}

func (s *Service) CheckRoot(ctx context.Context, raw string) error {
	return s.AuthenticateRoot(ctx, raw)
}

func (s *Service) CreateAdminWithRoot(ctx context.Context, rootRaw string, name string, expiresAt *time.Time) (TokenSecretResult, error) {
	if err := s.AuthenticateRoot(ctx, rootRaw); err != nil {
		return TokenSecretResult{}, err
	}
	permissions := permission.DefaultAdmin
	return s.createToken(ctx, CreateTokenRequest{Type: authtoken.TypeAdmin, Name: name, Permissions: &permissions, ExpiresAt: expiresAt})
}

func (s *Service) RevokeAllWithRoot(ctx context.Context, rootRaw string) (int64, error) {
	if err := s.AuthenticateRoot(ctx, rootRaw); err != nil {
		return 0, err
	}
	return s.tokens.RevokeAllTokens(ctx)
}

func (s *Service) CreateToken(ctx context.Context, actor Actor, req CreateTokenRequest) (TokenSecretResult, error) {
	if !actor.Permissions.Has(permission.TokenAdmin) {
		return TokenSecretResult{}, ErrForbidden
	}
	if req.Type != authtoken.TypeAdmin && req.Type != authtoken.TypeUser {
		return TokenSecretResult{}, ErrForbidden
	}
	if req.Permissions == nil {
		defaults := permission.DefaultUser
		if req.Type == authtoken.TypeAdmin {
			defaults = permission.DefaultAdmin
		}
		req.Permissions = &defaults
	}
	if err := req.Permissions.ValidateForType(string(req.Type)); err != nil {
		return TokenSecretResult{}, fmt.Errorf("%w: %s", ErrInvalidToken, err)
	}
	if !req.Permissions.IsSubset(actor.Permissions) {
		return TokenSecretResult{}, ErrForbidden
	}
	return s.createToken(ctx, req)
}

func (s *Service) RevokeToken(ctx context.Context, actor Actor, id uuid.UUID) (repo.Token, error) {
	if !actor.Permissions.Has(permission.TokenAdmin) {
		return repo.Token{}, ErrForbidden
	}
	tok, err := s.tokens.GetTokenByID(ctx, id)
	if err != nil {
		return repo.Token{}, err
	}
	if tok.Type == string(authtoken.TypeRoot) {
		return repo.Token{}, ErrForbidden
	}
	if tok.Type != string(authtoken.TypeAdmin) && tok.Type != string(authtoken.TypeUser) {
		return repo.Token{}, ErrForbidden
	}
	if !permission.Permission(tok.Permissions).IsSubset(actor.Permissions) {
		return repo.Token{}, ErrForbidden
	}
	if tok.Type == string(authtoken.TypeAdmin) && actor.TokenID == id {
		count, err := s.tokens.CountActiveAdminTokensExcluding(ctx, id)
		if err != nil {
			return repo.Token{}, err
		}
		if count == 0 {
			return repo.Token{}, ErrForbidden
		}
	}
	return s.tokens.RevokeToken(ctx, id)
}

func (s *Service) ListTokens(ctx context.Context, actor Actor, params repo.ListOperatorParams) ([]repo.Token, error) {
	if !actor.Permissions.Has(permission.TokenAdmin) {
		return nil, ErrForbidden
	}
	return s.tokens.ListTokens(ctx, params)
}

func (s *Service) GetToken(ctx context.Context, actor Actor, id uuid.UUID) (repo.Token, error) {
	if !actor.Permissions.Has(permission.TokenAdmin) {
		return repo.Token{}, ErrForbidden
	}
	return s.tokens.GetTokenByID(ctx, id)
}

func (s *Service) createToken(ctx context.Context, req CreateTokenRequest) (TokenSecretResult, error) {
	if req.Permissions == nil {
		return TokenSecretResult{}, fmt.Errorf("%w: permissions are required", ErrInvalidToken)
	}
	expiresAt, err := s.resolveExpiry(req.Type, req.ExpiresAt)
	if err != nil {
		return TokenSecretResult{}, err
	}
	id := uuid.Must(uuid.NewV7())
	name := strings.TrimSpace(req.Name)
	if name == "" {
		switch req.Type {
		case authtoken.TypeAdmin, authtoken.TypeUser:
			return TokenSecretResult{}, fmt.Errorf("%w: token name is required for %s tokens", ErrInvalidToken, req.Type)
		default:
			return TokenSecretResult{}, fmt.Errorf("%w: token name is required", ErrInvalidToken)
		}
	}
	raw, secret, err := authtoken.Generate(req.Type, id)
	if err != nil {
		return TokenSecretResult{}, err
	}
	created, err := s.tokens.CreateToken(ctx, repo.CreateTokenParams{
		ID:            id,
		Type:          string(req.Type),
		Name:          name,
		Permissions:   uint8(*req.Permissions),
		HashAlgorithm: s.hasher.Algorithm(),
		TokenHash:     s.hasher.Hash(secret),
		ExpiresAt:     expiresAt,
	})
	if err != nil {
		return TokenSecretResult{}, mapCreateTokenError(err)
	}
	return TokenSecretResult{Token: created, Raw: raw}, nil
}

func mapCreateTokenError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case pgerrcode.UniqueViolation:
		if pgErr.ConstraintName == "idx_tokens_one_root" {
			return ErrRootAlreadyExist
		}
		return ErrInvalidToken
	case pgerrcode.CheckViolation:
		return ErrInvalidToken
	default:
		return err
	}
}

func (s *Service) resolveExpiry(tokenType authtoken.Type, raw *time.Time) (time.Time, error) {
	cfg, ok := s.tokenTypes[tokenType]
	if !ok {
		return time.Time{}, fmt.Errorf("%w: unsupported token type", ErrInvalidToken)
	}
	now := s.now()
	expiresAt := now.Add(cfg.DefaultTTL)
	if raw != nil {
		expiresAt = *raw
	}
	if !expiresAt.After(now) {
		return time.Time{}, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidToken)
	}
	if expiresAt.After(now.Add(cfg.MaxTTL)) {
		return time.Time{}, fmt.Errorf("%w: expires_at exceeds max ttl", ErrInvalidToken)
	}
	return expiresAt, nil
}

func NormalizeRootSecret(raw string) (string, error) {
	secret := strings.TrimSpace(raw)
	if len(secret) < DefaultRootTokenMinLength {
		return "", ErrInvalidToken
	}
	for _, r := range secret {
		if unicode.IsSpace(r) {
			return "", ErrInvalidToken
		}
	}
	return secret, nil
}

func farFuture() time.Time {
	// Keep the value safely inside the timestamp range supported by every
	// JSON/SQL client while still making root tokens effectively permanent.
	return time.Date(2099, time.December, 31, 23, 59, 59, 0, time.UTC)
}
