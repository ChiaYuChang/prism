package appconfig

import (
	"fmt"
	"log/slog"
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
	Provider string        `mapstructure:"provider" yaml:"provider" validate:"required,oneof=gemini openai ollama"`
	Key      string        `mapstructure:"key"      yaml:"key"`
	Model    string        `mapstructure:"model"    yaml:"model"    validate:"required"`
	Timeout  time.Duration `mapstructure:"timeout"  yaml:"timeout"`

	// KeyFile is an optional path to a file containing the LLM API key.
	// When non-empty, ResolveSecrets reads the file and overrides Key,
	// matching the PostgresConfig.PasswordFile / ValkeyConfig.PasswordFile
	// pattern. Operators should mount the secret as a file (k8s Secret
	// volume / docker secrets / .secrets/) so the literal key never lands
	// in argv, env vars, or yaml committed to source.
	KeyFile string `mapstructure:"key-file" yaml:"key_file"`
}

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

// String renders a human-readable summary with the API key redacted.
func (c LLMConfig) String() string {
	return fmt.Sprintf("provider=%s model=%s key=%s timeout=%s",
		c.Provider, c.Model, prismlogger.SecretMask(c.Key), c.Timeout)
}

// LogValue redacts the API key when the config is logged via slog.Any.
func (c LLMConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("provider", c.Provider),
		slog.String("model", c.Model),
		slog.String("key", prismlogger.SecretMask(c.Key)),
		slog.Duration("timeout", c.Timeout),
	)
}
