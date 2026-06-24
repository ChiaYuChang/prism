package appconfig

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLLMConfig_NoSecretLeak — same shape as ValkeyConfig: fmt verbs and
// slog.Any must redact the API key. Any new secret field must extend
// String() / LogValue() and add a check here.
func TestLLMConfig_NoSecretLeak(t *testing.T) {
	const apiKey = "sk-abcdef-0123456789"
	cfg := LLMConfig{
		Provider: map[string]any{"openai": map[string]any{}},
		Key:      apiKey,
		Model:    "gpt-4o-mini",
	}

	for _, verb := range []string{"%v", "%+v", "%s"} {
		out := fmt.Sprintf(verb, cfg)
		assert.NotContains(t, out, apiKey, "verb %q leaked api key", verb)
	}

	var buf strings.Builder
	h := slog.NewTextHandler(&buf, nil)
	slog.New(h).Info("llm", slog.Any("config", cfg))
	logged := buf.String()
	assert.NotContains(t, logged, apiKey, "slog.Any leaked api key: %s", logged)
}

func TestLLMConfig_ResolveSecrets_FromFile(t *testing.T) {
	const expected = "key-from-file-xyz"
	path := filepath.Join(t.TempDir(), "llm-key")
	require.NoError(t, os.WriteFile(path, []byte(expected+"\n"), 0o600))

	cfg := LLMConfig{KeyFile: path, Key: "literal-should-be-overridden"}
	require.NoError(t, cfg.ResolveSecrets())
	assert.Equal(t, expected, cfg.Key)
}

func TestLLMConfig_ResolveSecrets_NoFile_KeepsKey(t *testing.T) {
	cfg := LLMConfig{Key: "inline-key"}
	require.NoError(t, cfg.ResolveSecrets())
	assert.Equal(t, "inline-key", cfg.Key)
}

func TestLLMConfig_ResolveSecrets_MissingFile_Errors(t *testing.T) {
	cfg := LLMConfig{KeyFile: filepath.Join(t.TempDir(), "missing")}
	err := cfg.ResolveSecrets()
	require.Error(t, err)
}

func TestLLMConfig_BindFlags_HyphenatedKeys(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("llm-key-file", "", "")
	require.NoError(t, fs.Parse([]string{
		"--llm-key-file=/run/secrets/llm-key",
	}))

	v := viper.New()
	require.NoError(t, LLMConfig{}.BindFlags(v, fs))

	var cfg struct {
		LLM LLMConfig `mapstructure:"llm"`
	}
	require.NoError(t, v.Unmarshal(&cfg))
	assert.Equal(t, "/run/secrets/llm-key", cfg.LLM.KeyFile)
}

func TestLLMConfig_ProviderName(t *testing.T) {
	cfg := LLMConfig{Provider: map[string]any{"opencode": map[string]any{"base_url": "http://opencode:4096"}}}

	name, err := cfg.ProviderName()
	require.NoError(t, err)
	assert.Equal(t, "opencode", name)

	providerCfg, err := cfg.ProviderConfig()
	require.NoError(t, err)
	assert.Equal(t, "http://opencode:4096", providerCfg["base_url"])
}

func TestLLMConfig_ProviderNameErrors(t *testing.T) {
	_, err := (LLMConfig{}).ProviderName()
	require.ErrorIs(t, err, ErrLLMProviderMissing)

	_, err = (LLMConfig{Provider: map[string]any{"gemini": map[string]any{}, "openai": map[string]any{}}}).ProviderName()
	require.ErrorIs(t, err, ErrLLMProviderAmbiguous)

	_, err = (LLMConfig{Provider: map[string]any{"not-real": map[string]any{}}}).ProviderName()
	require.ErrorIs(t, err, ErrLLMProviderUnsupported)
}
