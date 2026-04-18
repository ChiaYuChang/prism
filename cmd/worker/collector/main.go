package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/internal/collector/fetcher"
	"github.com/ChiaYuChang/prism/internal/collector/minifier"
	"github.com/ChiaYuChang/prism/internal/collector/parser"
	"github.com/ChiaYuChang/prism/internal/collector/transformer"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
)

const (
	TracerName = "prism.worker.collector"
)

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
	tracer := infra.Tracer()

	monitor := obs.NewHealthMonitor()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	obs.StartHealthServer(ctx, config.HealthPort, monitor)

	msgr, err := config.Messenger.NewMessenger(logger)
	if err != nil {
		logger.Error("failed to initialize messenger", "type", config.MessengerType, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize messenger")
		os.Exit(1)
	}
	defer func() {
		if err := msgr.Close(); err != nil {
			logger.Error("failed to close messenger", "error", err)
		}
	}()

	dbRepo, dbRepoCloser, err := pg.NewFactory(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "backend", "postgres", "host", config.Postgres.Host, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Postgres")
		os.Exit(1)
	}
	defer func() {
		if err := dbRepoCloser.Close(); err != nil {
			logger.Error("failed to close repository resources", "error", err)
		}
	}()

	httpClient := &http.Client{Timeout: config.HTTPTimeout}
	pageFetcher := fetcher.NewRetryFetcher(
		fetcher.NewHTTPFetcher(httpClient), 3, time.Second,
	)
	pageFetcher.
		Handle(http.StatusNotFound, fetcher.FailFastHandler).
		Handle(http.StatusForbidden, fetcher.FailFastHandler).
		Handle(http.StatusUnauthorized, fetcher.FailFastHandler)

	// Wire LocalArchiver as errorSaver when ArchiveDir is set.
	// When nil, raw content is not archived on Minify failures.
	var errorSaver collector.Saver
	if config.ArchiveDir != "" {
		arch, err := archiver.NewLocalArchiver(config.ArchiveDir, logger)
		if err != nil {
			logger.Error("failed to initialize archiver", "archive_dir", config.ArchiveDir, "error", err)
			monitor.SetStatus(obs.LevelError, "Failed to initialize archiver")
			os.Exit(1)
		}
		errorSaver = arch
		logger.Info("local archive enabled", "archive_dir", config.ArchiveDir)
	}

	handler, err := NewHandler(
		logger,
		tracer,
		pageFetcher,
		errorSaver,
		minifier.New(),
		transformer.NewNoOpTransformer(),
		parser.NewArticleParser(),
		msgr, // archivePublisher wired up to send messages to the archive topic
		dbRepo.Pipeline(),
		dbRepo.Scheduler(),
	)
	if err != nil {
		logger.Error("failed to build collector handler", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to build collector handler")
		os.Exit(1)
	}

	messages, err := msgr.Subscribe(ctx, message.TaskTopic)
	if err != nil {
		logger.Error("failed to subscribe topic", "topic", message.TaskTopic, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to subscribe task topic")
		os.Exit(1)
	}

	logger.Info("collector worker started",
		"topic", message.TaskTopic,
		"messenger", config.MessengerType,
		"http_timeout", config.HTTPTimeout,
	)
	monitor.OK()

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down collector worker")
			return
		case msg, ok := <-messages:
			if !ok {
				logger.Warn("message channel closed")
				return
			}

			msgCtx, cancel := context.WithTimeout(ctx, config.MaxProcessingTime)
			ack, err := handler.HandleMessage(msgCtx, msg)
			cancel()
			if err != nil {
				logger.Error("failed to handle collector task", "error", err)
			}

			if ack {
				msg.Ack()
				continue
			}
			msg.Nack()
		}
	}
}
