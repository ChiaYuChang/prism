package config

import (
	"fmt"
	"os"

	"github.com/ChiaYuChang/prism/internal/collector/parser/html"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Version int                     `yaml:"version" json:"version,omitempty"`
	Parsers map[string]ParserConfig `yaml:"parsers" json:"parsers,omitempty"`
}

type ParserConfig struct {
	Enabled     *bool           `yaml:"enabled"      json:"enabled,omitempty"`
	JSONLD      bool            `yaml:"jsonld"       json:"jsonld,omitempty"`
	DateLayouts []string        `yaml:"date_layouts" json:"date_layouts,omitempty"`
	HTML        html.RuleConfig `yaml:"html"         json:"html,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open parser config: %w", err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode parser config: %w", err)
	}

	return cfg, nil
}
