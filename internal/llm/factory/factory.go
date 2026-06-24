// Package factory builds LLM providers from an appconfig.LLMConfig.
// Lives in a subpackage so it can import the concrete provider packages
// (gemini / openai / ollama) without creating an import cycle on the
// parent llm package.
package factory

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/llm"
	"github.com/ChiaYuChang/prism/internal/llm/gemini"
	"github.com/ChiaYuChang/prism/internal/llm/ollama"
	"github.com/ChiaYuChang/prism/internal/llm/openai"
	"github.com/ChiaYuChang/prism/internal/llm/opencode"
	"github.com/go-playground/mold/v4"
	"github.com/go-playground/validator/v10"
	"go.opentelemetry.io/otel"
)

const defaultTimeout = 30 * time.Second

var providerDecoders = map[string]llm.ProviderConfigDecoder{
	"gemini":   gemini.Decoder{},
	"openai":   openai.Decoder{},
	"ollama":   ollama.Decoder{},
	"opencode": opencode.Decoder{},
}

// NewGenerator instantiates an llm.Generator from the supplied LLMConfig.
// Promoted from cmd/worker/planner so the same construction path is shared
// by every command that needs a generator (planner, collector fallback,
// recover, parse-probe).
func NewGenerator(ctx context.Context, cfg appconfig.LLMConfig, logger *slog.Logger) (llm.Generator, error) {
	return NewProvider(ctx, cfg, logger)
}

// NewEmbedder instantiates an instrumented llm.Embedder from the supplied LLMConfig.
func NewEmbedder(ctx context.Context, cfg appconfig.LLMConfig, logger *slog.Logger) (llm.Embedder, error) {
	return NewProvider(ctx, cfg, logger)
}

// NewProvider instantiates an instrumented llm.Provider from the supplied LLMConfig.
func NewProvider(ctx context.Context, cfg appconfig.LLMConfig, logger *slog.Logger) (llm.Provider, error) {
	providerName, err := cfg.ProviderName()
	if err != nil {
		return nil, err
	}
	provider, err := newProvider(ctx, cfg, logger, providerName)
	if err != nil {
		return nil, err
	}
	metrics, err := llm.NewMetrics(otel.Meter("prism.llm"))
	if err != nil {
		return nil, fmt.Errorf("create LLM metrics: %w", err)
	}
	return llm.InstrumentProvider(provider, metrics, "llm."+providerName), nil
}

func newProvider(ctx context.Context, cfg appconfig.LLMConfig, logger *slog.Logger, providerName string) (llm.Provider, error) {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	rawCfg, err := cfg.ProviderConfig()
	if err != nil {
		return nil, err
	}

	decoder, ok := providerDecoders[providerName]
	if !ok {
		return nil, fmt.Errorf("unsupported LLM provider: %s", providerName)
	}
	providerCfg, err := decoder.Decode(rawCfg)
	if err != nil {
		return nil, fmt.Errorf("decode %s config: %w", providerName, err)
	}

	return providerCfg.Build(ctx, llm.BuildDeps{
		Logger:      logger,
		Tracer:      infra.Tracer(),
		Validator:   validator.New(),
		Transformer: mold.New(),
		HTTPClient:  &http.Client{Timeout: timeout},
	}, llm.BuildConfig{
		Model:   cfg.Model,
		Key:     cfg.Key,
		Timeout: timeout,
	})
}
