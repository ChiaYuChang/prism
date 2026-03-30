package config_test

import (
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery/scout/config"
	htmlscout "github.com/ChiaYuChang/prism/internal/discovery/scout/html"
	"github.com/stretchr/testify/assert"
)

func TestConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: config.Config{
				Version: 1,
				Scout: config.ScoutConfig{
					HTML: config.HTMLSection{
						Scouts: []config.HTMLScoutConfig{
							{
								Name:   "dpp",
								Format: "html",
								Hosts:  []string{"dpp.org.tw"},
								Rules: []htmlscout.RuleConfig{
									{
										ItemSelector:  ".item",
										TitleSelector: "h1",
										LinkSelector:  "a",
										LinkAttr:      "href",
									},
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			cfg: config.Config{
				Version: 1,
				Scout: config.ScoutConfig{
					HTML: config.HTMLSection{
						Scouts: []config.HTMLScoutConfig{
							{
								Format: "html",
								Hosts:  []string{"dpp.org.tw"},
								Rules: []htmlscout.RuleConfig{
									{
										ItemSelector:  ".item",
										TitleSelector: "h1",
										LinkSelector:  "a",
										LinkAttr:      "href",
									},
								},
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
				Scout: config.ScoutConfig{
					RSS: config.FeedSection{
						Scouts: []config.FeedScoutConfig{
							{
								Name:   "cna",
								Format: "html", // Should be rss or atom
								Hosts:  []string{"cna.com.tw"},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing hosts",
			cfg: config.Config{
				Version: 1,
				Scout: config.ScoutConfig{
					Atom: config.FeedSection{
						Scouts: []config.FeedScoutConfig{
							{
								Name:   "kmt",
								Format: "atom",
								Hosts:  []string{}, // Missing hosts
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.New(tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
