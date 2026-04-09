package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	htmlscout "github.com/ChiaYuChang/prism/internal/discovery/scout/html"
	"gopkg.in/yaml.v3"
)

const CurrentVersion = 1

type Config struct {
	Version int         `yaml:"version" json:"version" validate:"required,min=1"`
	Scout   ScoutConfig `yaml:"scout"   json:"scout"   validate:"required"`
}

// Write writes the Config to an io.Writer in the specified format (json, yaml, yml).
func (c Config) Write(w io.Writer, format string) error {
	var data []byte
	var err error

	switch strings.ToLower(format) {
	case "json":
		data, err = json.MarshalIndent(c, "", "  ")
	case "yaml", "yml":
		data, err = yaml.Marshal(c)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	if err != nil {
		return fmt.Errorf("encode config (%s): %w", format, err)
	}

	_, err = w.Write(data)
	return err
}

// WriteFile writes the Config to a file path, inferring format from the extension.
func (c Config) WriteFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	format := strings.TrimPrefix(filepath.Ext(path), ".")
	return c.Write(f, format)
}

type ScoutConfig struct {
	HTML   HTMLSection   `yaml:"html"   json:"html"`
	RSS    FeedSection   `yaml:"rss"    json:"rss"`
	Atom   FeedSection   `yaml:"atom"   json:"atom"`
	Custom CustomSection `yaml:"custom" json:"custom"`
}

type HTMLSection struct {
	Defaults HTMLDefaults      `yaml:"defaults" json:"defaults"`
	Scouts   []HTMLScoutConfig `yaml:"scouts"   json:"scouts" validate:"dive"`
}

type FeedSection struct {
	Defaults FeedDefaults      `yaml:"defaults" json:"defaults"`
	Scouts   []FeedScoutConfig `yaml:"scouts"   json:"scouts" validate:"dive"`
}

type CustomSection struct {
	Defaults CustomDefaults      `yaml:"defaults" json:"defaults"`
	Scouts   []CustomScoutConfig `yaml:"scouts"   json:"scouts" validate:"dive"`
}

type HTMLDefaults struct {
	Enabled *bool             `yaml:"enabled" json:"enabled"`
	Headers map[string]string `yaml:"headers" json:"headers"`
}

type FeedDefaults struct {
	Enabled *bool             `yaml:"enabled" json:"enabled"`
	Headers map[string]string `yaml:"headers" json:"headers"`
}

type CustomDefaults struct {
	Enabled *bool `yaml:"enabled" json:"enabled"`
}

type HTMLScoutConfig struct {
	Enabled  *bool                  `yaml:"enabled"   json:"enabled"`
	Name     string                 `yaml:"name"      json:"name"      validate:"required"`
	Format   string                 `yaml:"format"    json:"format"    validate:"omitempty,oneof=html"`
	SpanName string                 `yaml:"span_name" json:"span_name"`
	Hosts    []string               `yaml:"hosts"     json:"hosts"     validate:"required,min=1"`
	Headers  map[string]string      `yaml:"headers"   json:"headers"`
	Rules    []htmlscout.RuleConfig `yaml:"rules"     json:"rules"     validate:"required,min=1"`
}

type FeedScoutConfig struct {
	Enabled  *bool             `yaml:"enabled"   json:"enabled"`
	Name     string            `yaml:"name"      json:"name"      validate:"required"`
	Format   string            `yaml:"format"    json:"format"    validate:"omitempty,oneof=rss atom"`
	SpanName string            `yaml:"span_name" json:"span_name"`
	Hosts    []string          `yaml:"hosts"     json:"hosts"     validate:"required,min=1"`
	Headers  map[string]string `yaml:"headers"   json:"headers"`
}

type CustomScoutConfig struct {
	Enabled  *bool             `yaml:"enabled"   json:"enabled"`
	Name     string            `yaml:"name"      json:"name"      validate:"required"`
	Format   string            `yaml:"format"    json:"format"    validate:"omitempty,oneof=custom"`
	SpanName string            `yaml:"span_name" json:"span_name"`
	Hosts    []string          `yaml:"hosts"     json:"hosts"     validate:"required,min=1"`
	Headers  map[string]string `yaml:"headers"   json:"headers"`
}
