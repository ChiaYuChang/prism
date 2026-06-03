package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigSearchTargetsFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := []byte(`
llm:
  model: gemini-test
search:
  targets:
    yahoo:
      enable: true
      source_abbr: yahoo
      url: https://tw.news.yahoo.com
      site: tw.news.yahoo.com
`)
	require.NoError(t, os.WriteFile(path, body, 0600))

	cfg, err := LoadConfig([]string{"--config", path})
	require.NoError(t, err)
	targets := cfg.Search.EnabledTargets()
	require.Len(t, targets, 1)
	require.Equal(t, "yahoo", targets[0].SourceAbbr)
	require.Equal(t, "tw.news.yahoo.com", targets[0].Site)
}

func TestLoadConfigSearchTargetsFromJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := []byte(`{
  "llm": {"model": "gemini-test"},
  "search": {
    "targets": {
      "yahoo": {"enable": true, "source_abbr": "yahoo", "url": "https://tw.news.yahoo.com", "site": "tw.news.yahoo.com"}
    }
  }
}`)
	require.NoError(t, os.WriteFile(path, body, 0600))

	cfg, err := LoadConfig([]string{"--config", path})
	require.NoError(t, err)
	targets := cfg.Search.EnabledTargets()
	require.Len(t, targets, 1)
	require.Equal(t, "https://tw.news.yahoo.com", targets[0].URL)
}
