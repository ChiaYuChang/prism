package pg

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgx-contrib/pgxotel"
	"github.com/pgx-contrib/pgxprom"
	"github.com/pgx-contrib/pgxtrace"
	"github.com/prometheus/client_golang/prometheus"
)

// customEnumTypes lists every user-defined enum in db/schema.sql. pgx v5 does
// not auto-derive array codecs from scalar codecs, so each enum's array
// counterpart (`_<name>`) must be registered too — otherwise queries like
// `kind = ANY($1::task_kind[])` fail at encode time with
// `cannot find encode plan` because the OID has no Codec in the TypeMap.
// Add new enums here when introduced.
var customEnumTypes = []string{
	"candidate_ingestion_method",
	"content_type",
	"embedding_category",
	"entity_type",
	"model_type",
	"source_type",
	"task_kind",
	"task_status",
}

func registerCustomTypes(ctx context.Context, conn *pgx.Conn) error {
	for _, name := range customEnumTypes {
		for _, n := range []string{name, "_" + name} {
			t, err := conn.LoadType(ctx, n)
			if err != nil {
				return fmt.Errorf("load pg type %q: %w", n, err)
			}
			conn.TypeMap().RegisterType(t)
		}
	}
	return nil
}

// Pool defaults. These override pgx's library defaults to favour short-lived
// workers — the prior 30-minute idle timeout kept connections pinned to
// low-traffic processes (e.g. the scheduler ticks every 10m).
const (
	defaultMaxConns        int32         = 4
	defaultMinConns        int32         = 0
	defaultMaxConnIdleTime time.Duration = 1 * time.Minute
	defaultMaxConnLifetime time.Duration = 30 * time.Minute
)

// ErrDBMetricsInitFailed indicates pgx Prometheus metrics initialization failed.
var ErrDBMetricsInitFailed = errors.New("db metrics init failed")

var (
	queryCollector      *pgxprom.QueryCollector
	poolCollector       *pgxprom.PoolCollector
	registerMetricsOnce sync.Once
	metricsInitErr      error
)

func initDBMetrics() error {
	registerMetricsOnce.Do(func() {
		queryCollector = pgxprom.NewQueryCollector()
		poolCollector = pgxprom.NewPoolCollector()

		// Register collectors with the default Prometheus registerer.
		if err := prometheus.Register(queryCollector); err != nil {
			metricsInitErr = fmt.Errorf(
				"%w: register pgx query collector with prometheus: %w",
				ErrDBMetricsInitFailed, err)
			return
		}
		if err := prometheus.Register(poolCollector); err != nil {
			metricsInitErr = fmt.Errorf(
				"%w: register pgx pool collector with prometheus: %w",
				ErrDBMetricsInitFailed, err)
			return
		}
	})
	return metricsInitErr
}

type Builder struct {
	config appconfig.PostgresConfig
}

func NewRepositoryBuilder(config appconfig.PostgresConfig) *Builder {
	return &Builder{config: config}
}

func (f *Builder) NewRepository(ctx context.Context) (repo.Repository, repo.Closer, error) {
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

	// Build the composite tracer list using global defaults
	tracers := []pgx.QueryTracer{
		&pgxotel.QueryTracer{
			Name: "prism.repository",
		},
	}
	if f.config.MetricsEnabled {
		// Initialize DB metrics once lazy-loaded.
		if err := initDBMetrics(); err != nil {
			return nil, nil, err
		}
		tracers = append(tracers, queryCollector)
	}

	// Combine all tracers using pgxtrace
	poolCfg.ConnConfig.Tracer = pgxtrace.CompositeQueryTracer(tracers)
	poolCfg.AfterConnect = registerCustomTypes

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("connect postgres: %w", err)
	}

	if f.config.MetricsEnabled {
		// Register the pool stats with the Prometheus collector.
		poolCollector.Add(pool)
	}

	return NewPostgresRepository(pool), repo.CloseFunc(func() error {
		if f.config.MetricsEnabled {
			poolCollector.Remove(pool)
		}
		pool.Close()
		return nil
	}), nil
}
