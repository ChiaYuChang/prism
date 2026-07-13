package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/discovery/extractor"
	"github.com/ChiaYuChang/prism/internal/discovery/planner"
	"github.com/ChiaYuChang/prism/internal/infra"
	llmfactory "github.com/ChiaYuChang/prism/internal/llm/factory"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/prompt"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
)

const (
	TracerName = "prism.worker.planner"
)

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
	tracer := telemetry.Tracer(TracerName)
	infra.SetTracer(tracer)

	monitor := obs.NewHealthMonitor()
	obs.StartHealthServer(ctx, config.HealthPort, monitor)
	go func() {
		<-ctx.Done()
		monitor.SetStatus(obs.LevelWarn, "shutting down")
	}()

	msgr, err := config.Messenger.NewMessenger(logger)
	if err != nil {
		logger.Error("failed to initialize messenger", "type", config.MessengerType, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize messenger")
		os.Exit(1)
	}
	defer func() { _ = msgr.Close() }()

	dbRepo, dbRepoCloser, err := pg.NewRepositoryBuilder(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "backend", "postgres", "host", config.Postgres.Host, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Postgres")
		os.Exit(1)
	}
	defer func() { _ = dbRepoCloser.Close() }()

	providerName, err := config.LLM.ProviderName()
	if err != nil {
		logger.Error("failed to resolve LLM provider", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to resolve LLM provider")
		os.Exit(1)
	}
	warnIfOllamaUnavailable(ctx, config.LLM, providerName, logger)

	generator, err := llmfactory.NewGenerator(ctx, config.LLM, logger)
	if err != nil {
		logger.Error("failed to initialize LLM generator", "provider", providerName, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize LLM generator")
		os.Exit(1)
	}

	promptBody, promptLogAttrs, err := loadPlannerPrompt(ctx, dbRepo.Prompts(), *config)
	if err != nil {
		logger.Error("failed to load prompt", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to load prompt")
		os.Exit(1)
	}

	ext, err := extractor.NewExtractor(generator, logger, tracer, config.LLM.Model, string(promptBody))
	if err != nil {
		logger.Error("failed to initialize extractor", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize extractor")
		os.Exit(1)
	}

	plan, err := planner.New(logger, tracer, ext, dbRepo.Tasks(), dbRepo.Pipeline())
	if err != nil {
		logger.Error("failed to initialize planner", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize planner")
		os.Exit(1)
	}

	targets := config.Search.EnabledTargets()
	handler, err := NewHandler(logger, tracer, plan, targets)
	if err != nil {
		logger.Error("failed to initialize handler", "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize handler")
		os.Exit(1)
	}

	messages, err := msgr.Subscribe(ctx, message.BatchCompletedTopic)
	if err != nil {
		logger.Error("failed to subscribe topic", "topic", message.BatchCompletedTopic, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to subscribe batch completed topic")
		os.Exit(1)
	}

	logger.Info("planner worker started",
		"topic", message.BatchCompletedTopic,
		"messenger", config.MessengerType,
		"llm_provider", providerName,
		"llm_model", config.LLM.Model,
	)
	logger.Info("planner prompt loaded", promptLogAttrs...)
	monitor.OK()

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down planner worker")
			return
		case msg, ok := <-messages:
			if !ok {
				logger.Warn("message channel closed")
				return
			}
			if ctx.Err() != nil {
				logger.Info("returning unaccepted batch completed signal during shutdown")
				msg.Nack()
				return
			}

			msgCtx, cancel := infra.NewDrainContext(config.ShutdownTimeout)
			ack, err := handler.HandleMessage(msgCtx, msg)
			cancel()
			if err != nil {
				logger.Error("failed to handle batch completed signal", "error", err)
			}

			if ack {
				msg.Ack()
			} else {
				msg.Nack()
			}
		}
	}
}

func warnIfOllamaUnavailable(ctx context.Context, config appconfig.LLMConfig, providerName string, logger *slog.Logger) {
	if providerName != "ollama" {
		return
	}
	providerConfig, err := config.ProviderConfig()
	if err != nil {
		return
	}
	baseURL, _ := providerConfig["base_url"].(string)
	if baseURL == "" {
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, baseURL, nil)
	if err != nil {
		logger.Warn("invalid Ollama base URL", "base_url", baseURL, "error", err)
		return
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		logger.Warn("Ollama endpoint unavailable at startup", "base_url", baseURL, "error", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		logger.Warn("Ollama endpoint returned an error at startup", "base_url", baseURL, "status", resp.StatusCode)
	}
}

func loadPlannerPrompt(ctx context.Context, prompts repo.Prompts, config Config) ([]byte, []any, error) {
	if config.Prompt.Enabled() {
		body, version, err := prompt.Resolve(ctx, prompts, config.Prompt)
		if err != nil {
			return nil, nil, err
		}
		return body, []any{
			"prompt_id", version.ID.String(),
			"prompt_key", version.Key,
			"prompt_version", version.Version,
			"prompt_hash", version.Hash,
			"prompt_path", version.Path,
		}, nil
	}
	body, err := os.ReadFile(config.PromptPath)
	if err != nil {
		return nil, nil, err
	}
	return body, []any{"prompt_path", config.PromptPath}, nil
}
