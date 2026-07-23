package pg

import (
	"context"
	"time"

	"github.com/ChiaYuChang/prism/internal/repo"
)

type PGRootControl struct {
	db DBTX
}

var _ repo.RootControl = (*PGRootControl)(nil)

func (r *PGRepository) RootControl() repo.RootControl {
	return &PGRootControl{db: r.db}
}

func (r *PGRootControl) InitRoot(ctx context.Context, arg repo.CreateRootControlParams) (repo.Token, error) {
	row := r.db.QueryRow(ctx, `SELECT * FROM prism_root_init($1, $2, $3, $4, $5)`, arg.ID, arg.Name, arg.HashAlgorithm, arg.TokenHash, arg.ExpiresAt)
	return scanRootControlToken(row)
}

func (r *PGRootControl) CheckRoot(ctx context.Context, arg repo.RootAuthParams) (bool, error) {
	var valid bool
	err := r.db.QueryRow(ctx, `SELECT prism_root_check($1, $2)`, arg.HashAlgorithm, arg.TokenHash).Scan(&valid)
	return valid, err
}

func (r *PGRootControl) CreateAdmin(ctx context.Context, arg repo.CreateRootAdminParams) (repo.Token, error) {
	row := r.db.QueryRow(ctx, `SELECT * FROM prism_root_create_admin($1, $2, $3, $4, $5, $6, $7)`, arg.RootAuthParams.HashAlgorithm, arg.RootAuthParams.TokenHash, arg.ID, arg.Name, arg.HashAlgorithm, arg.TokenHash, arg.ExpiresAt)
	return scanRootControlToken(row)
}

func (r *PGRootControl) RevokeAll(ctx context.Context, arg repo.RootAuthParams) (int64, error) {
	var count int64
	err := r.db.QueryRow(ctx, `SELECT prism_root_revoke_all($1, $2)`, arg.HashAlgorithm, arg.TokenHash).Scan(&count)
	return count, err
}

type rootControlRow interface {
	Scan(dest ...any) error
}

func scanRootControlToken(row rootControlRow) (repo.Token, error) {
	var (
		token                 repo.Token
		createdAt, expiresAt  time.Time
		lastUsedAt, renewedAt *time.Time
		rotatedAt, revokedAt  *time.Time
	)
	err := row.Scan(
		&token.ID, &token.Type, &token.Name, &token.Permissions,
		&token.HashAlgorithm, &createdAt, &expiresAt,
		&lastUsedAt, &renewedAt, &rotatedAt, &revokedAt,
	)
	if err != nil {
		return repo.Token{}, err
	}
	token.CreatedAt = createdAt
	token.ExpiresAt = expiresAt
	token.LastUsedAt = lastUsedAt
	token.RenewedAt = renewedAt
	token.RotatedAt = rotatedAt
	token.RevokedAt = revokedAt
	return token, nil
}
