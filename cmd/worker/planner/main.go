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

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/discovery/extractor"
	"github.com/ChiaYuChang/prism/internal/discovery/planner"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/llm/gemini"
	"github.com/ChiaYuChang/prism/internal/llm/ollama"
	"github.com/ChiaYuChang/prism/internal/llm/openai"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo/pg"
	"github.com/go-playground/mold/v4"
	"github.com/go-playground/validator/v10"
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
	defer func() { _ = msgr.Close() }()

	dbRepo, dbRepoCloser, err := pg.NewFactory(config.Postgres).NewRepository(ctx)
	if err != nil {
		logger.Error("failed to initialize repository", "backend", "postgres", "host", config.Postgres.Host, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to connect to Postgres")
		os.Exit(1)
	}
	defer func() { _ = dbRepoCloser.Close() }()

	generator, err := newGenerator(ctx, config.LLM, logger)
	if err != nil {
		logger.Error("failed to initialize LLM generator", "provider", config.LLM.Provider, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to initialize LLM generator")
		os.Exit(1)
	}

	prompt, err := os.ReadFile(config.PromptPath)
	if err != nil {
		logger.Error("failed to read prompt file", "path", config.PromptPath, "error", err)
		monitor.SetStatus(obs.LevelError, "Failed to read prompt file")
		os.Exit(1)
	}

	ext, err := extractor.NewExtractor(generator, logger, tracer, config.LLM.Model, string(prompt))
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

	handler, err := NewHandler(logger, tracer, plan, dbRepo.Scout())
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
		"llm_provider", config.LLM.Provider,
		"llm_model", config.LLM.Model,
		"prompt_path", config.PromptPath,
	)
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

			ack, err := handler.HandleMessage(ctx, msg)
			if err != nil {
				logger.Error("failed to handle batch completed signal", "error", err)
			}

			if ack {
				msg.Ack()
				continue
			}
			msg.Nack()
		}
	}
}

// newGenerator instantiates an llm.Generator from the given LLMConfig.
func newGenerator(ctx context.Context, cfg appconfig.LLMConfig, logger *slog.Logger) (llm.Generator, error) {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	v := validator.New()
	m := mold.New()
	hc := &http.Client{Timeout: timeout}

	switch cfg.Provider {
	case "gemini":
		return gemini.New(ctx, logger, infra.Tracer(), v, m, hc, gemini.Config{
			APIKey:  cfg.Key,
			Timeout: timeout,
		})
	case "openai":
		return openai.New(ctx, logger, infra.Tracer(), v, m, hc, openai.Config{
			APIKey:  cfg.Key,
			Timeout: timeout,
		})
	case "ollama":
		return ollama.New(ctx, logger, infra.Tracer(), v, m, hc, ollama.Config{
			Timeout: timeout,
		})
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.Provider)
	}
}
