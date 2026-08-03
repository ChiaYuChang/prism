package appconfig

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
)

// LLMConfig holds provider-agnostic LLM settings shared across worker commands.
// Flag prefix: llm-*  →  viper key prefix: llm.*  →  parsers.yaml key path: fallback.llm.*
//
// Mapstructure tags drive viper-bound flag/env loading. YAML tags drive direct
// yaml.v3 decoding (used when this struct is embedded under a `fallback.llm:`
// block in parsers.yaml). Both are present so a single LLMConfig can be loaded
// either way without translation.
type LLMConfig struct {
	Provider map[string]any `mapstructure:"provider" yaml:"provider" validate:"required"`
	Key      string         `mapstructure:"key"      yaml:"key"`
	Model    string         `mapstructure:"model"    yaml:"model"    validate:"required"`
	Timeout  time.Duration  `mapstructure:"timeout"  yaml:"timeout"`

	// KeyFile is an optional path to a file containing the LLM API key.
	// When non-empty, ResolveSecrets reads the file and overrides Key,
	// matching the PostgresConfig.PasswordFile / ValkeyConfig.PasswordFile
	// pattern. Operators should mount the secret as a file (k8s Secret
	// volume / docker secrets / .secrets/) so the literal key never lands
	// in argv, env vars, or yaml committed to source.
	KeyFile string `mapstructure:"key-file" yaml:"key_file"`
}

var (
	ErrLLMProviderMissing     = errors.New("llm provider is missing")
	ErrLLMProviderAmbiguous   = errors.New("llm provider is ambiguous")
	ErrLLMProviderUnsupported = errors.New("llm provider is unsupported")
)

// ResolveSecrets loads KeyFile if set, replacing Key. Call after viper
// unmarshal / yaml decode and before validator.
func (c *LLMConfig) ResolveSecrets() error {
	v, err := LoadFromFile(c.KeyFile)
	if err != nil {
		return err
	}
	if v != "" {
		c.Key = v
	}
	return nil
}

// ProviderName returns the single selected provider name.
func (c LLMConfig) ProviderName() (string, error) {
	if len(c.Provider) == 0 {
		return "", ErrLLMProviderMissing
	}
	if len(c.Provider) > 1 {
		providers := make([]string, 0, len(c.Provider))
		for name := range c.Provider {
			providers = append(providers, name)
		}
		sort.Strings(providers)
		return "", fmt.Errorf("%w: %v", ErrLLMProviderAmbiguous, providers)
	}
	for name := range c.Provider {
		switch name {
		case "gemini", "openai", "ollama", "opencode":
			return name, nil
		default:
			return "", fmt.Errorf("%w: %s", ErrLLMProviderUnsupported, name)
		}
	}
	return "", ErrLLMProviderMissing
}

// ProviderConfig returns the raw provider-specific config for the selected provider.
func (c LLMConfig) ProviderConfig() (map[string]any, error) {
	name, err := c.ProviderName()
	if err != nil {
		return nil, err
	}
	raw := c.Provider[name]
	if raw == nil {
		return map[string]any{}, nil
	}
	providerCfg, ok := raw.(map[string]any)
	if ok {
		return providerCfg, nil
	}
	return nil, fmt.Errorf("llm provider config for %s must be a map", name)
}

// String renders a human-readable summary with the API key redacted.
func (c LLMConfig) String() string {
	provider, err := c.ProviderName()
	if err != nil {
		provider = "invalid"
	}
	return fmt.Sprintf("provider=%s model=%s key=%s timeout=%s",
		provider, c.Model, prismlogger.SecretMask(c.Key), c.Timeout)
}

// LogValue redacts the API key when the config is logged via slog.Any.
func (c LLMConfig) LogValue() slog.Value {
	provider, err := c.ProviderName()
	if err != nil {
		provider = "invalid"
	}
	return slog.GroupValue(
		slog.String("provider", provider),
		slog.String("model", c.Model),
		slog.String("key", prismlogger.SecretMask(c.Key)),
		slog.Duration("timeout", c.Timeout),
	)
}
