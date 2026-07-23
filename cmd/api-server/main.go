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
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/ChiaYuChang/prism/cmd/api-server/docs"
	"github.com/ChiaYuChang/prism/internal/appconfig"
	prismauth "github.com/ChiaYuChang/prism/internal/auth"
	"github.com/ChiaYuChang/prism/internal/auth/permission"
	authtoken "github.com/ChiaYuChang/prism/internal/auth/token"
	prismhttp "github.com/ChiaYuChang/prism/internal/http"
	"github.com/ChiaYuChang/prism/internal/http/api"
	"github.com/ChiaYuChang/prism/internal/http/middleware"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/infra/natsadmin"
	"github.com/ChiaYuChang/prism/internal/infra/natsdiag"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	logger := obs.NewLoggerFromHandlers(handlers)
	slog.SetDefault(logger)
	appconfig.FlushPendingLogs()
	if config.Monitoring.Mode == "push" {
		logger.Warn("Monitoring mode 'push' is configured, but it is not yet fully implemented/supported across workers/apps/api")
	}
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

	valkeyNeeded := config.Cache.Enabled || config.Monitoring.Backend == "valkey" || config.SchedulerControl.Enabled
	var valkeyClient *redis.Client
	if valkeyNeeded {
		valkeyClient, err = infra.NewValkeyClient(ctx, infra.ValkeyClientConfig{
			Addr:           config.Valkey.Addr(),
			Username:       config.Valkey.Username,
			Password:       config.Valkey.Password,
			DB:             config.Valkey.DB,
			ClientName:     config.Valkey.ClientName,
			TracingEnabled: config.Valkey.TracingEnabled,
			MetricsEnabled: config.Valkey.MetricsEnabled,
		})
		if err != nil {
			logger.Error("failed to dial valkey", "addr", config.Valkey.Addr(), "error", err)
			os.Exit(1)
		}
		defer func() { _ = valkeyClient.Close() }()
	}

	statusMonitor := api.StatusMonitor(api.NewInMemoryMonitor(config.Monitoring.Mode))
	if config.Monitoring.Backend == "valkey" {
		statusMonitor, err = api.NewValkeyMonitor(valkeyClient, config.Monitoring.Mode, config.Monitoring.StatusKey)
		if err != nil {
			logger.Error("failed to construct valkey status monitor", "error", err)
			os.Exit(1)
		}
		logger.Info("status monitor backend enabled", "backend", "valkey", "key", config.Monitoring.StatusKey)
	}

	serverOpts := []api.ServerOption{
		api.WithOperator(repository.Operator()),
		api.WithSources(repository.Sources()),
		api.WithStatusMonitor(statusMonitor),
	}
	natsInspector, err := natsdiag.New(natsdiag.Config{
		Host: config.NATS.Host, Port: config.NATS.Port,
		Username: config.NATS.Username, Password: config.NATS.Password,
		Token: config.NATS.Token,
	})
	if err != nil {
		logger.Error("failed to configure NATS diagnostics", "error", err)
		os.Exit(1)
	}
	serverOpts = append(serverOpts, api.WithNATSInspector(natsInspector))
	natsAdmin, err := natsadmin.New(natsadmin.Config{
		Host: config.NATS.Host, Port: config.NATS.Port,
		Username: config.NATS.Username, Password: config.NATS.Password,
		Token: config.NATS.Token,
	})
	if err != nil {
		logger.Error("failed to configure NATS administration", "error", err)
		os.Exit(1)
	}
	serverOpts = append(serverOpts, api.WithNATSAdmin(natsAdmin))
	if config.SchedulerControl.Enabled {
		toggles, err := infra.NewSchedulerToggleStore(valkeyClient)
		if err != nil {
			logger.Error("failed to construct scheduler toggle store", "error", err)
			os.Exit(1)
		}
		serverOpts = append(serverOpts, api.WithSchedulerToggles(toggles))
	}

	if config.Cache.Enabled {
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
	hasher, herr := authtoken.NewHasher(config.Auth.HashAlgorithm)
	if herr != nil {
		logger.Error("failed to initialize token hasher", "error", herr)
		os.Exit(1)
	}
	tokenTypes := make(map[string]api.TokenTypeConfig, len(config.Auth.TokenTypes))
	for name, cfg := range config.Auth.TokenTypes {
		tokenTypes[name] = api.TokenTypeConfig{Prefix: cfg.Prefix, DefaultTTL: cfg.DefaultTTL, MaxTTL: cfg.MaxTTL}
	}
	authenticator, aerr := prismauth.NewAuthenticator(prismauth.AuthenticatorParams{
		Store:        repository.Tokens(),
		AllowedTypes: []authtoken.Type{authtoken.TypeAdmin, authtoken.TypeUser},
	})
	if aerr != nil {
		logger.Error("failed to initialize token authenticator", "error", aerr)
		os.Exit(1)
	}
	tokenAuth := middleware.TokenAuthMiddleware(middleware.TokenAuthenticator{Authenticator: authenticator})
	publicAuthMiddleware := []middleware.Middleware{tokenAuth, middleware.RequirePermissions(permission.UserAPI)}
	adminAuthMiddleware := []middleware.Middleware{tokenAuth, middleware.RequirePermissions(permission.AdminAPI)}
	serverOpts = append(serverOpts, api.WithTokens(repository.Tokens(), hasher, tokenTypes))
	promptStore, err := appconfig.NewStorage(ctx, config.Prompts.StorageURI, config.S3)
	if err != nil {
		logger.Error("failed to initialize prompt storage", "error", err)
		os.Exit(1)
	}
	serverOpts = append(serverOpts, api.WithPrompts(repository.Prompts(), promptStore))

	apiServer, err := api.NewServer(logger, repository.Scout(), repository.Tasks(), repository.Pipeline(), repository.UserFetches(), serverOpts...)
	if err != nil {
		logger.Error("failed to construct api server", "error", err)
		os.Exit(1)
	}

	var expectedServices []string
	targets := make(map[string]api.MonitorTarget)
	for name, target := range config.Monitoring.Targets {
		if target.IsEnabled() {
			expectedServices = append(expectedServices, name)
			if target.DisplayName == "" {
				target.DisplayName = name
			}
			targets[name] = target.MonitorTarget
		}
	}
	apiServer.InitializeStatuses(expectedServices)

	if config.Monitoring.Mode == "pull" {
		apiServer.StartMonitor(ctx, config.Monitoring.Interval, targets)
	}

	rootRouter := prismhttp.NewRouter(
		middleware.RequestID(),
		middleware.HTTPTracing(),
		middleware.HTTPMetrics(httpMetrics),
		middleware.Logger(logger),
		middleware.Recoverer(logger),
		middleware.CORS(middleware.CORSOptions{
			AllowOrigins: config.CORSOrigins,
			AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
			AllowHeaders: []string{"Content-Type", "Authorization", middleware.RequestIDHeader, middleware.TokenAuthHeader},
			MaxAgeSecs:   600,
		}),
	)
	rootRouter.HandleFunc("GET /healthz", livenessHandler(monitor))
	rootRouter.HandleFunc("GET /readyz", readinessHandler(monitor))
	rootRouter.Handle("GET /swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	rootRouter.Route("/api/v1", func(apiV1Router *prismhttp.Router) {
		apiServer.RegisterV1(apiV1Router)
	}, publicAuthMiddleware...)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", config.Port),
		Handler:      rootRouter.Handler(),
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	}

	serverErr := make(chan error, 3)
	go func() {
		logger.Info("api server listening", "port", config.Port)
		monitor.OK()
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("public api server failed: %w", err)
		}
	}()
	var adminServer *http.Server
	if config.Admin.Enabled {
		adminRouter := prismhttp.NewRouter(
			middleware.RequestID(),
			middleware.HTTPTracing(),
			middleware.HTTPMetrics(httpMetrics),
			middleware.Logger(logger),
			middleware.Recoverer(logger),
			middleware.CORS(middleware.CORSOptions{
				AllowOrigins: config.CORSOrigins,
				AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
				AllowHeaders: []string{"Content-Type", "Authorization", middleware.RequestIDHeader, middleware.TokenAuthHeader},
				MaxAgeSecs:   600,
			}),
		)
		adminRouter.HandleFunc("GET /healthz", livenessHandler(monitor))
		adminRouter.HandleFunc("GET /readyz", readinessHandler(monitor))
		adminRouter.Route("/api/v1", func(apiV1Router *prismhttp.Router) {
			apiV1Router.Route("/admin", func(admin *prismhttp.Router) {
				apiServer.RegisterV1Admin(admin)
			}, adminAuthMiddleware...)
		})
		adminServer = &http.Server{
			Addr:         fmt.Sprintf(":%d", config.Admin.Port),
			Handler:      adminRouter.Handler(),
			ReadTimeout:  config.ReadTimeout,
			WriteTimeout: config.WriteTimeout,
		}
		go func() {
			logger.Info("admin api server listening", "port", config.Admin.Port)
			if err := adminServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErr <- fmt.Errorf("admin api server failed: %w", err)
			}
		}()
	}

	internalMux := http.NewServeMux()
	if config.Monitoring.Mode == "push" {
		apiServer.RegisterInternal(internalMux, adminAuthMiddleware...)
	}

	// Register pprof handlers internally on the internal port for secure monitoring
	internalMux.HandleFunc("/debug/pprof/", pprof.Index)
	internalMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	internalMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	internalMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	internalMux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	internalMux.Handle("/metrics", promhttp.Handler())

	internalServer := &http.Server{
		Addr: fmt.Sprintf(":%d", config.Monitoring.InternalPort),
		Handler: middleware.Chain(
			middleware.RequestID(),
			middleware.HTTPTracing(),
			middleware.HTTPMetrics(httpMetrics),
			middleware.Logger(logger),
			middleware.Recoverer(logger),
		)(internalMux),
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	}

	go func() {
		logger.Info("internal api server listening", "port", config.Monitoring.InternalPort)
		if err := internalServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("internal api server failed: %w", err)
		}
	}()

	select {
	case err := <-serverErr:
		logger.Error("api server failed", "error", err)
		os.Exit(1)
	case <-ctx.Done():
		monitor.SetStatus(obs.LevelWarn, "shutting down")
		logger.Info("shutting down api server")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()

	var errs []error
	if err := server.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("public server shutdown: %w", err))
	}
	if adminServer != nil {
		if err := adminServer.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("admin server shutdown: %w", err))
		}
	}
	if internalServer != nil {
		if err := internalServer.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("internal server shutdown: %w", err))
		}
	}
	if len(errs) > 0 {
		logger.Error("graceful shutdown failed", "errors", errors.Join(errs...))
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
