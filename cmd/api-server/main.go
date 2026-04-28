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
	httpSwagger "github.com/swaggo/http-swagger"
)

const TracerName = "prism.api-server"

func main() {
	config, err := LoadConfig(os.Args[1:])
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger, logFile, err := obs.InitLogger(config.Logger.Path, config.Logger.GetLogLevel())
	if err != nil {
		slog.Error("failed to initialize logger", "error", err)
		os.Exit(1)
	}
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
	}

	shutdownTracer := infra.InitAndSetTracer(TracerName)
	defer func() {
		if err := shutdownTracer(context.Background()); err != nil {
			logger.Error("failed to shutdown tracer", "error", err)
		}
	}()

	monitor := obs.NewHealthMonitor()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repository, repositoryCloser, err := pg.NewRepositoryBuilder(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "backend", "postgres", "host", config.Postgres.Host, "error", err)
		os.Exit(1)
	}
	defer func() { _ = repositoryCloser.Close() }()

	apiServer, err := api.NewServer(logger, repository.Scout(), repository.Tasks(), repository.Pipeline())
	if err != nil {
		logger.Error("failed to construct api server", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", livenessHandler(monitor))
	mux.HandleFunc("GET /readyz", readinessHandler(monitor))
	mux.Handle("GET /swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	apiServer.Register(mux)

	chain := middleware.Chain(
		middleware.RequestID(),
		middleware.Logger(logger),
		middleware.Recoverer(logger),
		middleware.CORS(middleware.CORSOptions{
			AllowOrigins: config.CORSOrigins,
			AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowHeaders: []string{"Content-Type", "Authorization", middleware.RequestIDHeader},
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
