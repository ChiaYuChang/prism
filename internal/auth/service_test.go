package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/auth/permission"
	authtoken "github.com/ChiaYuChang/prism/internal/auth/token"
	"github.com/ChiaYuChang/prism/internal/repo"
	repomocks "github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCreateAdminUsesPersistedTokenID(t *testing.T) {
	tokens := repomocks.NewMockTokens(t)
	rootRaw := strings.Repeat("r", DefaultRootTokenMinLength)
	rootHash, err := authtoken.NewHasher("sha256")
	require.NoError(t, err)
	root := repo.Token{
		ID:            uuid.New(),
		Type:          string(authtoken.TypeRoot),
		HashAlgorithm: rootHash.Algorithm(),
		TokenHash:     rootHash.Hash([]byte(rootRaw)),
		ExpiresAt:     time.Now().Add(time.Hour),
	}
	tokens.EXPECT().GetRootToken(mock.Anything).Return(root, nil)

	service, err := NewService(ServiceParams{
		Tokens: tokens,
		Hasher: rootHash,
		TokenTypes: map[authtoken.Type]TokenTypeConfig{
			authtoken.TypeAdmin: {DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour},
		},
	})
	require.NoError(t, err)
	var persisted repo.Token
	tokens.EXPECT().CreateToken(mock.Anything, mock.AnythingOfType("repo.CreateTokenParams")).RunAndReturn(func(_ context.Context, arg repo.CreateTokenParams) (repo.Token, error) {
		persisted = repo.Token{ID: arg.ID, Type: arg.Type, HashAlgorithm: arg.HashAlgorithm, TokenHash: arg.TokenHash, ExpiresAt: time.Now().Add(time.Hour)}
		return persisted, nil
	})

	result, err := service.CreateAdminWithRoot(context.Background(), rootRaw, "ci-admin", nil)
	require.NoError(t, err)
	parsed, err := authtoken.Parse(result.Raw)
	require.NoError(t, err)
	require.Equal(t, persisted.ID, parsed.ID)
}

func TestInitRootIsIdempotentCheckPath(t *testing.T) {
	tokens := repomocks.NewMockTokens(t)
	tokens.EXPECT().GetRootToken(mock.Anything).Return(repo.Token{}, pgx.ErrNoRows)
	tokens.EXPECT().CreateToken(mock.Anything, mock.AnythingOfType("repo.CreateTokenParams")).Return(repo.Token{ID: uuid.New(), ExpiresAt: time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)}, nil)
	hasher, err := authtoken.NewHasher("sha256")
	require.NoError(t, err)
	service, err := NewService(ServiceParams{Tokens: tokens, Hasher: hasher})
	require.NoError(t, err)
	_, err = service.InitRoot(context.Background(), strings.Repeat("r", DefaultRootTokenMinLength))
	require.NoError(t, err)
}

func TestCreateHumanTokenRequiresName(t *testing.T) {
	tokens := repomocks.NewMockTokens(t)
	hasher, err := authtoken.NewHasher("sha256")
	require.NoError(t, err)
	service, err := NewService(ServiceParams{
		Tokens: tokens,
		Hasher: hasher,
		TokenTypes: map[authtoken.Type]TokenTypeConfig{
			authtoken.TypeUser: {DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour},
		},
	})
	require.NoError(t, err)

	_, err = service.CreateToken(context.Background(), Actor{Type: authtoken.TypeAdmin, Permissions: permission.DefaultAdmin}, CreateTokenRequest{Type: authtoken.TypeUser})
	require.ErrorIs(t, err, ErrInvalidToken)
	require.ErrorContains(t, err, "token name is required for user tokens")
}

func TestCreateWorkerIsRejected(t *testing.T) {
	tokens := repomocks.NewMockTokens(t)
	hasher, err := authtoken.NewHasher("sha256")
	require.NoError(t, err)
	service, err := NewService(ServiceParams{
		Tokens: tokens,
		Hasher: hasher,
	})
	require.NoError(t, err)
	_, err = service.CreateToken(context.Background(), Actor{Type: authtoken.TypeAdmin, Permissions: permission.DefaultAdmin}, CreateTokenRequest{Type: authtoken.Type("worker")})
	require.ErrorIs(t, err, ErrForbidden)
}
