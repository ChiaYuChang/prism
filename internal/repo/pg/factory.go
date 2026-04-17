package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool defaults. These override pgx's library defaults to favour short-lived
// workers — the prior 30-minute idle timeout kept connections pinned to
// low-traffic processes (e.g. the scheduler ticks every 10m).
const (
	defaultMaxConns        int32         = 4
	defaultMinConns        int32         = 0
	defaultMaxConnIdleTime time.Duration = 1 * time.Minute
	defaultMaxConnLifetime time.Duration = 30 * time.Minute
)

type Factory struct {
	config appconfig.PostgresConfig
}

func NewFactory(config appconfig.PostgresConfig) *Factory {
	return &Factory{config: config}
}

func (f *Factory) NewRepository(ctx context.Context) (repo.Repository, repo.Closer, error) {
	poolCfg, err := pgxpool.ParseConfig(f.config.ConnString())
	if err != nil {
		return nil, nil, fmt.Errorf("parse postgres config: %w", err)
	}

	poolCfg.MaxConns = defaultMaxConns
	if f.config.MaxConns > 0 {
		poolCfg.MaxConns = f.config.MaxConns
	}
	poolCfg.MinConns = defaultMinConns
	if f.config.MinConns > 0 {
		poolCfg.MinConns = f.config.MinConns
	}
	poolCfg.MaxConnIdleTime = defaultMaxConnIdleTime
	if f.config.MaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = f.config.MaxConnIdleTime
	}
	poolCfg.MaxConnLifetime = defaultMaxConnLifetime
	if f.config.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = f.config.MaxConnLifetime
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("connect postgres: %w", err)
	}

	return NewPostgresRepository(pool), repo.CloseFunc(func() error {
		pool.Close()
		return nil
	}), nil
}
