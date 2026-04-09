package pg

import (
	"context"
	"fmt"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Factory struct {
	config appconfig.PostgresConfig
}

func NewFactory(config appconfig.PostgresConfig) *Factory {
	return &Factory{config: config}
}

func (f *Factory) NewRepository(ctx context.Context) (repo.Repository, repo.Closer, error) {
	pool, err := pgxpool.New(ctx, f.config.ConnString())
	if err != nil {
		return nil, nil, fmt.Errorf("connect postgres: %w", err)
	}

	return NewPostgresRepository(pool), repo.CloseFunc(func() error {
		pool.Close()
		return nil
	}), nil
}
