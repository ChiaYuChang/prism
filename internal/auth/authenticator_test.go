package auth

import (
	"context"
	"testing"
	"time"

	authtoken "github.com/ChiaYuChang/prism/internal/auth/token"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type authenticatorTokenStore struct {
	token repo.Token
}

func (s authenticatorTokenStore) GetTokenByID(context.Context, uuid.UUID) (repo.Token, error) {
	return s.token, nil
}

func (s authenticatorTokenStore) GetRootToken(context.Context) (repo.Token, error) {
	return repo.Token{}, nil
}

func TestAuthenticateTokenIncludesName(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	hasher, err := authtoken.NewHasher("sha256")
	require.NoError(t, err)
	raw, secret, err := authtoken.Generate(authtoken.TypeAdmin, id)
	require.NoError(t, err)
	authenticator, err := NewAuthenticator(AuthenticatorParams{
		Store: authenticatorTokenStore{token: repo.Token{
			ID:            id,
			Type:          string(authtoken.TypeAdmin),
			Name:          "alice-cli",
			HashAlgorithm: hasher.Algorithm(),
			TokenHash:     hasher.Hash(secret),
			ExpiresAt:     time.Now().Add(time.Hour),
		}},
		AllowedTypes: []authtoken.Type{authtoken.TypeAdmin},
	})
	require.NoError(t, err)

	principal, err := authenticator.AuthenticateToken(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, id, principal.TokenID)
	require.Equal(t, "alice-cli", principal.Name)
}
