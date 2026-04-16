package config

import (
	"github.com/ChiaYuChang/prism/internal/collector/parser/html"
)

type Config struct {
	Version int                     `yaml:"version"`
	Parsers map[string]ParserConfig `yaml:"parsers"`
}

type ParserConfig struct {
	Enabled     *bool          `yaml:"enabled"`
	JSONLD      bool           `yaml:"jsonld"`
	DateLayouts []string       `yaml:"date_layouts"`
	HTML        html.RuleConfig `yaml:"html"`
}
