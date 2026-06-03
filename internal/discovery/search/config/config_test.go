package config

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConfigDecodeYAMLAndJSON(t *testing.T) {
	yamlBody := []byte(`
targets:
  yahoo:
    enable: true
    source_abbr: yahoo
    url: https://tw.news.yahoo.com
    site: tw.news.yahoo.com
provider:
  brave:
    enable: true
    api_key: brave-key
  google-cse:
    enable: true
    api_key: google-key
    cx: cx-id
`)

	var ycfg Config
	require.NoError(t, yaml.Unmarshal(yamlBody, &ycfg))
	require.True(t, ycfg.Targets["yahoo"].Enable)
	require.True(t, ycfg.Provider.Brave.Enable)
	require.True(t, ycfg.Provider.GoogleCSE.Enable)
	require.Equal(t, "cx-id", ycfg.Provider.GoogleCSE.CX)

	jsonBody := []byte(`{
  "targets": {"yahoo": {"enable": true, "source_abbr": "yahoo", "url": "https://tw.news.yahoo.com", "site": "tw.news.yahoo.com"}},
  "provider": {"brave": {"enable": true, "api_key": "brave-key"}, "google-cse": {"enable": true, "api_key": "google-key", "cx": "cx-id"}}
}`)
	var jcfg Config
	require.NoError(t, json.Unmarshal(jsonBody, &jcfg))
	require.True(t, jcfg.Targets["yahoo"].Enable)
	require.Equal(t, "google-key", jcfg.Provider.GoogleCSE.APIKey)
}

func TestConfigResolveSecretsFileOverridesInline(t *testing.T) {
	path := t.TempDir() + "/brave-key"
	require.NoError(t, os.WriteFile(path, []byte("file-key\n"), 0600))
	cfg := Config{Provider: ProviderConfig{Brave: BraveConfig{APIKey: "inline-key", APIKeyFile: path}}}

	require.NoError(t, cfg.ResolveSecrets(slog.New(slog.NewTextHandler(io.Discard, nil))))
	require.Equal(t, "file-key", cfg.Provider.Brave.APIKey)
}

func TestConfigResolveSecretsInlineWarnsMasked(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	cfg := Config{Provider: ProviderConfig{Brave: BraveConfig{APIKey: "abcdefghijklmnopqrstuvwxyz"}}}

	require.NoError(t, cfg.ResolveSecrets(logger))
	log := buf.String()
	require.Contains(t, log, "prefer api_key_file")
	require.NotContains(t, log, "abcdefghijklmnopqrstuvwxyz")
	require.Contains(t, log, strings.Repeat("●", 20))
}
