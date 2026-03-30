package config_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery/backfiller/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_RoundTrip_Stream(t *testing.T) {
	ori := config.Config{
		Version: config.CurrentVersion,
		Backfiller: config.BackfillSection{
			Sources: map[string]config.SourceConfig{
				"abc": {
					SourceID: 1,
					Format:   "html",
					Pager: config.PagerConfig{
						Type:        "index",
						URLTemplate: "http://example.com/{{.Value}}",
						First:       0,
						Step:        10,
						Mode:        "index",
						Params: map[string]string{
							"a": "1",
							"b": "2",
						},
						OmitFirst: true,
					},
				},
			},
		},
	}

	buf := new(bytes.Buffer)
	err := ori.Write(buf, "json")
	require.NoError(t, err)

	cfgJSON, err := config.Read(buf, "json")
	require.NoError(t, err)
	assert.Equal(t, ori.Version, cfgJSON.Version)

	repoJSON, err := config.New(cfgJSON)
	require.NoError(t, err)
	_, ok := repoJSON.Source("abc")
	assert.True(t, ok)

	buf.Reset()
	err = ori.Write(buf, "yaml")
	require.NoError(t, err)

	cfgYAML, err := config.Read(buf, "yaml")
	require.NoError(t, err)
	assert.Equal(t, ori.Version, cfgYAML.Version)
}

func TestConfig_RoundTrip_File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_backfillers.yaml")
	cfgOri := config.Config{
		Version: config.CurrentVersion,
		Backfiller: config.BackfillSection{
			Sources: map[string]config.SourceConfig{
				"test-source": {
					SourceID: 99,
					Format:   "rss",
					Pager: config.PagerConfig{
						Type:        "index",
						URLTemplate: "http://test.com/{{.Value}}",
						Step:        1,
						Mode:        "index",
					},
				},
			},
		},
	}

	err := cfgOri.WriteFile(path)
	require.NoError(t, err)

	cfgFinal, err := config.ReadFile(path)
	require.NoError(t, err)

	assert.Equal(t, cfgOri.Version, cfgFinal.Version)

	repoFinal, err := config.New(cfgFinal)
	require.NoError(t, err)
	spec, ok := repoFinal.Source("test-source")
	assert.True(t, ok)
	assert.Equal(t, int32(99), spec.SourceID)
	assert.Equal(t, "rss", spec.Format)
}

func TestConfig_UnsupportedFormat(t *testing.T) {
	cfg := config.Config{Version: config.CurrentVersion}
	err := cfg.Write(new(bytes.Buffer), "toml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")

	_, err = config.Read(bytes.NewReader([]byte("{}")), "toml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}
