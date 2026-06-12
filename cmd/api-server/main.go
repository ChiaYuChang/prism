// Package main is the Prism HTTP API server.
//
// @title          Prism API
// @version        0.1
// @description    User-facing read API for Prism candidate/content data.
// @BasePath       /api/v1
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/ChiaYuChang/prism/cmd/api-server/docs"
	"github.com/ChiaYuChang/prism/internal/http/api"
	"github.com/ChiaYuChang/prism/internal/http/middleware"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
	"github.com/redis/go-redis/v9"
	httpSwagger "github.com/swaggo/http-swagger"
)

const TracerName = "prism.api-server"

func main() {
	config, err := LoadConfig(os.Args[1:])
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handlers, logFile, shutdownLogger, err := obs.BuildLoggingHandlers(ctx, config.Logger)
	if err != nil {
		slog.Error("failed to initialize logger", "error", err)
		os.Exit(1)
	}
	logger := prismlogger.NewLoggerFromHandlers(handlers)
	slog.SetDefault(logger)
	defer func() {
		if err := shutdownLogger(context.Background()); err != nil {
			logger.Error("failed to shutdown logger", "error", err)
		}
	}()
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
	}

	telemetry, err := obs.InitTelemetry(ctx, config.Telemetry)
	if err != nil {
		logger.Error("failed to initialize telemetry", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := telemetry.Shutdown(context.Background()); err != nil {
			logger.Error("failed to shutdown telemetry", "error", err)
		}
	}()
	infra.SetTracer(telemetry.Tracer(TracerName))
	httpMetrics, err := middleware.NewHTTPMetrics(telemetry.Meter(TracerName))
	if err != nil {
		logger.Error("failed to initialize HTTP metrics", "error", err)
		os.Exit(1)
	}

	monitor := obs.NewHealthMonitor()

	repository, repositoryCloser, err := pg.NewRepositoryBuilder(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "backend", "postgres", "host", config.Postgres.Host, "error", err)
		os.Exit(1)
	}
	defer func() { _ = repositoryCloser.Close() }()

	var serverOpts []api.ServerOption

	if config.Cache.Enabled {
		valkeyClient, err := infra.NewValkeyClient(ctx, &redis.Options{
			Addr:     config.Valkey.Addr(),
			Username: config.Valkey.Username,
			Password: config.Valkey.Password,
			DB:       config.Valkey.DB,
		})
		if err != nil {
			logger.Error("failed to dial valkey for progress cache", "addr", config.Valkey.Addr(), "error", err)
			os.Exit(1)
		}
		defer func() { _ = valkeyClient.Close() }()
		cache, err := api.NewValkeyProgressCache(valkeyClient, config.Cache.LiveTTL, config.Cache.TerminalTTL)
		if err != nil {
			logger.Error("failed to construct progress cache", "error", err)
			os.Exit(1)
		}
		serverOpts = append(serverOpts, api.WithProgressCache(cache))
		logger.Info("progress cache enabled",
			"valkey_addr", config.Valkey.Addr(),
			"live_ttl", config.Cache.LiveTTL,
			"terminal_ttl", config.Cache.TerminalTTL)
	}

	if config.RateLimit.Enabled {
		limiter := middleware.NewInMemoryIPLimiter(
			config.RateLimit.RPS,
			config.RateLimit.Burst,
			config.RateLimit.IPCacheSize,
		)
		serverOpts = append(serverOpts, api.WithGetFetchLimiter(limiter))
		logger.Info("get-fetch rate limit enabled",
			"rps", config.RateLimit.RPS,
			"burst", config.RateLimit.Burst,
			"ip_cache_size", config.RateLimit.IPCacheSize)
	}
	authTokens, err := config.Auth.Token.TokenSet()
	if err != nil {
		logger.Error("failed to load auth tokens", "error", err)
		os.Exit(1)
	}
	var apiMiddleware []middleware.Middleware
	if len(authTokens) > 0 {
		apiMiddleware = append(apiMiddleware, middleware.TokenListAuth(authTokens))
		logger.Info("api token auth enabled", "tokens", len(authTokens))
	}

	apiServer, err := api.NewServer(logger, repository.Scout(), repository.Tasks(), repository.Pipeline(), repository.UserFetches(), serverOpts...)
	if err != nil {
		logger.Error("failed to construct api server", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", livenessHandler(monitor))
	mux.HandleFunc("GET /readyz", readinessHandler(monitor))
	mux.Handle("GET /swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	apiServer.Register(mux, apiMiddleware...)

	chain := middleware.Chain(
		middleware.RequestID(),
		middleware.HTTPMetrics(httpMetrics),
		middleware.Logger(logger),
		middleware.Recoverer(logger),
		middleware.CORS(middleware.CORSOptions{
			AllowOrigins: config.CORSOrigins,
			AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowHeaders: []string{"Content-Type", "Authorization", middleware.RequestIDHeader, middleware.AuthTokenHeader},
			MaxAgeSecs:   600,
		}),
	)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", config.Port),
		Handler:      chain(mux),
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("api server listening", "port", config.Port)
		monitor.OK()
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		logger.Error("api server failed", "error", err)
		os.Exit(1)
	case <-ctx.Done():
		logger.Info("shutting down api server")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

// livenessHandler reports process liveness. Returns 200 as long as the process serves traffic.
//
// @Summary   Liveness probe
// @Tags      health
// @Produce   json
// @Success   200 {object} obs.HealthStatus
// @Router    /healthz [get]
func livenessHandler(monitor *obs.HealthMonitor) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		level, message := monitor.Status()
		writeHealth(w, http.StatusOK, level, message, monitor.Uptime())
	}
}

// readinessHandler reports whether the server is ready to serve traffic.
//
// @Summary   Readiness probe
// @Tags      health
// @Produce   json
// @Success   200 {object} obs.HealthStatus
// @Failure   503 {object} obs.HealthStatus
// @Router    /readyz [get]
func readinessHandler(monitor *obs.HealthMonitor) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		level, message := monitor.Status()
		status := http.StatusOK
		if level != obs.LevelOK {
			status = http.StatusServiceUnavailable
		}
		writeHealth(w, status, level, message, monitor.Uptime())
	}
}

func writeHealth(w http.ResponseWriter, status int, level obs.HealthLevel, message string, uptime time.Duration) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(obs.HealthStatus{
		Level:     level,
		Message:   message,
		Uptime:    uptime.Truncate(time.Second).String(),
		Timestamp: time.Now(),
	})
}
