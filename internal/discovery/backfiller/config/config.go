package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const CurrentVersion = 1

type Config struct {
	Version    int             `yaml:"version"    json:"version"    validate:"required,min=1"`
	Backfiller BackfillSection `yaml:"backfiller" json:"backfiller" validate:"required"`
}

type BackfillSection struct {
	Sources map[string]SourceConfig `yaml:"sources" json:"sources" validate:"required,dive"`
}

type SourceConfig struct {
	Name     string        `yaml:"-"         json:"-"`
	SourceID int32         `yaml:"source_id" json:"source_id" validate:"required"`
	Format   string        `yaml:"format"    json:"format"    validate:"required,oneof=html rss atom custom"`
	BaseURL  string        `yaml:"base_url"  json:"base_url"  validate:"required,url"`
	Pager    PagerConfig   `yaml:"pager"     json:"pager"     validate:"required"`
	Timeout  time.Duration `yaml:"timeout"   json:"timeout"   validate:"min=0"`
}

type PagerConfig struct {
	Type        string            `yaml:"type"         json:"type"         validate:"required,oneof=index"`
	URLTemplate string            `yaml:"url_template" json:"url_template" validate:"required"`
	First       int               `yaml:"first"        json:"first"        validate:"min=0"`
	Step        int               `yaml:"step"         json:"step"         validate:"required,min=1"`
	Mode        string            `yaml:"mode"         json:"mode"         validate:"required,oneof=index cursor date-range"`
	Params      map[string]string `yaml:"params"       json:"params"`
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
