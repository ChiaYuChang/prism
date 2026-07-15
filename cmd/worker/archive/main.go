package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/storage/objectstore"
)

const TracerName = "prism.worker.archive"

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
	defer func() { _ = shutdownLogger(context.Background()) }()
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
	}
	telemetry, err := obs.InitTelemetry(ctx, config.Telemetry)
	if err != nil {
		logger.Error("failed to initialize telemetry", "error", err)
		os.Exit(1)
	}
	defer func() { _ = telemetry.Shutdown(context.Background()) }()
	infra.SetTracer(telemetry.Tracer(TracerName))
	monitor := obs.NewHealthMonitor()
	obs.StartHealthServer(ctx, config.HealthPort, monitor)
	arch, err := openArchiver(ctx, config.Archive, config.S3, logger)
	if err != nil {
		logger.Error("failed to initialize archiver", "error", err)
		os.Exit(1)
	}
	handler, err := NewHandler(arch)
	if err != nil {
		logger.Error("failed to initialize archive handler", "error", err)
		os.Exit(1)
	}
	msgr, err := config.Messenger.NewMessenger(logger)
	if err != nil {
		logger.Error("failed to initialize messenger", "error", err)
		os.Exit(1)
	}
	defer func() { _ = msgr.Close() }()
	messages, err := msgr.Subscribe(ctx, message.ArchiveTopic)
	if err != nil {
		logger.Error("failed to subscribe topic", "topic", message.ArchiveTopic, "error", err)
		os.Exit(1)
	}
	monitor.OK()
	logger.Info("archive worker started", "topic", message.ArchiveTopic, "archive", config.Archive)
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}
			msgCtx, cancel := infra.NewDrainContext(config.ShutdownTimeout)
			ack, err := handler.HandleMessage(msgCtx, msg)
			cancel()
			if err != nil {
				logger.Error("failed to archive signal", "error", err)
			}
			if ack {
				msg.Ack()
			} else {
				msg.Nack()
			}
		}
	}
}

func openArchiver(ctx context.Context, uri string, s3cfg appconfig.S3Config, logger *slog.Logger) (archiver.Archiver, error) {
	if !strings.HasPrefix(uri, "s3://") {
		return nil, fmt.Errorf("archive URI must use s3: %q", uri)
	}
	u, err := url.Parse(uri)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid archive URI %q", uri)
	}
	client, err := s3cfg.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("build s3 client: %w", err)
	}
	if err := objectstore.EnsureBucket(ctx, client, u.Host); err != nil {
		return nil, err
	}
	return archiver.NewS3Archiver(client, u.Host, strings.TrimPrefix(u.Path, "/"), logger)
}
