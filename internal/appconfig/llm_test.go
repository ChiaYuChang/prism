package appconfig

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLLMConfig_NoSecretLeak — same shape as ValkeyConfig: fmt verbs and
// slog.Any must redact the API key. Any new secret field must extend
// String() / LogValue() and add a check here.
func TestLLMConfig_NoSecretLeak(t *testing.T) {
	const apiKey = "sk-abcdef-0123456789"
	cfg := LLMConfig{
		Provider: "openai",
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
