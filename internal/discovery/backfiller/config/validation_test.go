package config_test

import (
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery/backfiller/config"
	"github.com/stretchr/testify/assert"
)

func TestConfig_Validation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     config.Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: config.Config{
				Version: 1,
				Backfiller: config.BackfillSection{
					Sources: map[string]config.SourceConfig{
						"dpp": {
							SourceID: 1,
							Format:   "html",
							Pager: config.PagerConfig{
								Type:        "index",
								URLTemplate: "http://example.com/{{.Value}}",
								Step:        1,
								Mode:        "index",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid mode",
			cfg: config.Config{
				Version: 1,
				Backfiller: config.BackfillSection{
					Sources: map[string]config.SourceConfig{
						"dpp": {
							SourceID: 1,
							Format:   "html",
							Pager: config.PagerConfig{
								Type:        "index",
								URLTemplate: "http://example.com",
								Step:        1,
								Mode:        "unknown",
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing url template",
			cfg: config.Config{
				Version: 1,
				Backfiller: config.BackfillSection{
					Sources: map[string]config.SourceConfig{
						"dpp": {
							SourceID: 1,
							Format:   "html",
							Pager: config.PagerConfig{
								Type: "index",
								Step: 1,
								Mode: "index",
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid format",
			cfg: config.Config{
				Version: 1,
				Backfiller: config.BackfillSection{
					Sources: map[string]config.SourceConfig{
						"dpp": {
							SourceID: 1,
							Format:   "pdf",
							Pager: config.PagerConfig{
								Type:        "index",
								URLTemplate: "http://example.com",
								Step:        1,
								Mode:        "index",
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "malformed template",
			cfg: config.Config{
				Version: 1,
				Backfiller: config.BackfillSection{
					Sources: map[string]config.SourceConfig{
						"dpp": {
							SourceID: 1,
							Format:   "html",
							Pager: config.PagerConfig{
								Type:        "index",
								URLTemplate: "http://example.com/{{.Value}", // Missing closing brace
								Step:        1,
								Mode:        "index",
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := config.New(c.cfg)
			if c.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
