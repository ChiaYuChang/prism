package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/pkg/rss"
)

func main() {
	logger, f, err := obs.InitLogger("logs/rss.log", slog.LevelDebug)
	if err != nil {
		slog.Error("failed to initialize logger", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if f != nil {
		defer func() {
			if err := f.Close(); err != nil {
				slog.Error("failed to close log file", "error", err)
			}
		}()
	}
	mux := rss.Mux(logger)

	host := "localhost"
	port := 8000
	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", host, port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		sCtx, sCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer sCancel()
		_ = server.Shutdown(sCtx)
	}()

	logger.Info(
		"rss server start",
		slog.String("host", host),
		slog.Int("port", port),
	)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error(
			"failed to start rss server",
			"error", err.Error(),
		)
	}
	logger.Info(
		"server shutdown",
	)
}
