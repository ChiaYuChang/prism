package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/ChiaYuChang/prism/internal/discovery/backfiller"
	"github.com/ChiaYuChang/prism/internal/infra"
	"gopkg.in/yaml.v3"
)

var (
	ErrUnsupportedConfigVersion = errors.New("unsupported backfiller config version")
	ErrDuplicateSourceName      = errors.New("duplicate backfiller source name")
	ErrSourceNotFound           = errors.New("backfiller source not found")
)

type Repository struct {
	cfg      Config
	bySource map[string]SourceConfig
}

// Read reads Config from an io.Reader and returns a Config object.
func Read(r io.Reader, format string) (Config, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return Config{}, fmt.Errorf("read config stream: %w", err)
	}

	var cfg Config
	switch strings.ToLower(format) {
	case "json":
		err = json.Unmarshal(body, &cfg)
	case "yaml", "yml":
		err = yaml.Unmarshal(body, &cfg)
	default:
		return Config{}, fmt.Errorf("unsupported format: %s", format)
	}

	if err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", format, err)
	}

	return cfg, nil
}

// ReadFile reads Config from a file path and returns a Config object.
func ReadFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config file: %w", err)
	}
	defer func() { _ = f.Close() }()

	format := strings.TrimPrefix(filepath.Ext(path), ".")
	return Read(f, format)
}

func New(cfg Config) (*Repository, error) {
	if err := infra.Validator().Struct(cfg); err != nil {
		return nil, fmt.Errorf("validate backfiller config: %w", err)
	}

	if cfg.Version != CurrentVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedConfigVersion, cfg.Version)
	}

	repo := &Repository{
		cfg:      cfg,
		bySource: make(map[string]SourceConfig),
	}

	for rawName, source := range cfg.Backfiller.Sources {
		name := strings.TrimSpace(strings.ToLower(rawName))
		if _, ok := repo.bySource[name]; ok {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateSourceName, name)
		}

		// Validate template syntax
		if err := validateTemplates(name, source.Pager); err != nil {
			return nil, err
		}

		source.Name = name
		source.Format = strings.TrimSpace(strings.ToLower(source.Format))
		source.BaseURL = strings.TrimRight(strings.TrimSpace(source.BaseURL), "/")
		source.Pager.Type = strings.TrimSpace(strings.ToLower(source.Pager.Type))
		source.Pager.URLTemplate = strings.TrimSpace(source.Pager.URLTemplate)
		source.Pager.Mode = strings.TrimSpace(strings.ToLower(source.Pager.Mode))
		repo.bySource[name] = source
	}

	return repo, nil
}

func validateTemplates(sourceName string, pc PagerConfig) error {
	check := func(name, tmpl string) error {
		if tmpl == "" {
			return nil
		}
		_, err := template.New("check").
			Funcs(backfiller.TemplateFuncMap).
			Parse(tmpl)
		if err != nil {
			return fmt.Errorf("source %q has invalid %s template: %w", sourceName, name, err)
		}
		return nil
	}

	if err := check("url_template", pc.URLTemplate); err != nil {
		return err
	}

	for k, v := range pc.Params {
		if err := check("param "+k, v); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) Source(name string) (SourceConfig, bool) {
	spec, ok := r.bySource[strings.TrimSpace(strings.ToLower(name))]
	return spec, ok
}

func (r *Repository) Config() Config {
	return r.cfg
}
