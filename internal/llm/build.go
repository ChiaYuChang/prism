package llm

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-playground/mold/v4"
	"github.com/go-playground/validator/v10"
	"github.com/go-viper/mapstructure/v2"
	"go.opentelemetry.io/otel/trace"
)

// BuildConfig carries provider-agnostic settings needed to build an LLM provider.
type BuildConfig struct {
	Model   string
	Key     string
	Timeout time.Duration
}

// BuildDeps carries shared dependencies for provider construction.
type BuildDeps struct {
	Logger      *slog.Logger
	Tracer      trace.Tracer
	Validator   *validator.Validate
	Transformer *mold.Transformer
	HTTPClient  *http.Client
}

// ProviderConfig is a decoded provider-specific config that can build a provider.
type ProviderConfig interface {
	Build(ctx context.Context, deps BuildDeps, cfg BuildConfig) (Provider, error)
}

// ProviderConfigDecoder decodes raw provider-specific config into a typed config.
type ProviderConfigDecoder interface {
	Decode(raw map[string]any) (ProviderConfig, error)
}

// DecodeProviderConfig decodes a raw provider config map into out using mapstructure tags.
func DecodeProviderConfig(raw map[string]any, out any) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName:          "mapstructure",
		Result:           out,
		WeaklyTypedInput: true,
	})
	if err != nil {
		return err
	}
	return decoder.Decode(raw)
}
