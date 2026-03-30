package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ChiaYuChang/prism/internal/discovery"
	atomscout "github.com/ChiaYuChang/prism/internal/discovery/scout/atom"
	yahooscout "github.com/ChiaYuChang/prism/internal/discovery/scout/custom/yahoo"
	htmlscout "github.com/ChiaYuChang/prism/internal/discovery/scout/html"
	rssscout "github.com/ChiaYuChang/prism/internal/discovery/scout/rss"
	"github.com/ChiaYuChang/prism/internal/infra"
	"gopkg.in/yaml.v3"
)

var (
	ErrUnsupportedConfigVersion = errors.New("unsupported scout config version")
	ErrDuplicateScoutHost       = errors.New("duplicate scout host")
	ErrUnknownCustomScout       = errors.New("unknown custom scout")
	ErrScoutHostsEmpty          = errors.New("scout hosts is empty")
)

type Repository struct {
	cfg    Config
	html   map[string]HTMLSpec
	rss    map[string]FeedSpec
	atom   map[string]FeedSpec
	custom map[string]CustomSpec
	byHost map[string]string
}

type HTMLSpec struct {
	Enabled bool
	Hosts   []string
	Config  htmlscout.Config
}

type FeedSpec struct {
	Enabled bool
	Hosts   []string
	Config  any
}

type CustomSpec struct {
	Enabled bool
	Hosts   []string
	Config  any
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
	defer f.Close()

	format := strings.TrimPrefix(filepath.Ext(path), ".")
	return Read(f, format)
}

func New(cfg Config) (*Repository, error) {
	if err := infra.Validator().Struct(cfg); err != nil {
		return nil, fmt.Errorf("validate scout config: %w", err)
	}

	if cfg.Version != CurrentVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedConfigVersion, cfg.Version)
	}

	repo := &Repository{
		cfg:    cfg,
		html:   make(map[string]HTMLSpec),
		rss:    make(map[string]FeedSpec),
		atom:   make(map[string]FeedSpec),
		custom: make(map[string]CustomSpec),
		byHost: make(map[string]string),
	}

	if err := repo.loadHTML(cfg.Scout.HTML); err != nil {
		return nil, err
	}
	if err := repo.loadRSS(cfg.Scout.RSS); err != nil {
		return nil, err
	}
	if err := repo.loadAtom(cfg.Scout.Atom); err != nil {
		return nil, err
	}
	if err := repo.loadCustom(cfg.Scout.Custom); err != nil {
		return nil, err
	}

	return repo, nil
}

func (r *Repository) Config() Config {
	return r.cfg
}

func (r *Repository) HTML(name string) (HTMLSpec, bool) {
	spec, ok := r.html[name]
	return spec, ok
}

func (r *Repository) RSS(name string) (FeedSpec, bool) {
	spec, ok := r.rss[name]
	return spec, ok
}

func (r *Repository) Atom(name string) (FeedSpec, bool) {
	spec, ok := r.atom[name]
	return spec, ok
}

func (r *Repository) Custom(name string) (CustomSpec, bool) {
	spec, ok := r.custom[name]
	return spec, ok
}

func (r *Repository) loadHTML(section HTMLSection) error {
	for _, entry := range section.Scouts {
		enabled := resolveEnabled(section.Defaults.Enabled, entry.Enabled)
		cfg := htmlscout.Config{
			Name:     strings.TrimSpace(entry.Name),
			Format:   firstNonEmpty(entry.Format, "html"),
			SpanName: firstNonEmpty(entry.SpanName, discovery.ScoutDiscoverSpanName("html", entry.Name)),
			Headers:  mergeHeaders(section.Defaults.Headers, entry.Headers),
			Rules:    entry.Rules,
		}.Normalize()
		if err := cfg.Validate(); err != nil {
			return err
		}

		hosts, err := normalizeHosts(entry.Hosts)
		if err != nil {
			return fmt.Errorf("%s: %w", cfg.Name, err)
		}
		if enabled {
			if err := r.registerHosts("html", cfg.Name, hosts); err != nil {
				return err
			}
		}

		r.html[cfg.Name] = HTMLSpec{
			Enabled: enabled,
			Hosts:   hosts,
			Config:  cfg,
		}
	}

	return nil
}

func (r *Repository) loadRSS(section FeedSection) error {
	for _, entry := range section.Scouts {
		enabled := resolveEnabled(section.Defaults.Enabled, entry.Enabled)
		cfg := rssscout.Config{
			Name:     strings.TrimSpace(entry.Name),
			Format:   firstNonEmpty(entry.Format, "rss"),
			SpanName: firstNonEmpty(entry.SpanName, discovery.ScoutDiscoverSpanName("rss", entry.Name)),
		}.Normalize()
		if err := cfg.Validate(); err != nil {
			return err
		}

		hosts, err := normalizeHosts(entry.Hosts)
		if err != nil {
			return fmt.Errorf("%s: %w", cfg.Name, err)
		}
		if enabled {
			if err := r.registerHosts("rss", cfg.Name, hosts); err != nil {
				return err
			}
		}

		r.rss[cfg.Name] = FeedSpec{
			Enabled: enabled,
			Hosts:   hosts,
			Config:  cfg,
		}
	}

	return nil
}

func (r *Repository) loadAtom(section FeedSection) error {
	for _, entry := range section.Scouts {
		enabled := resolveEnabled(section.Defaults.Enabled, entry.Enabled)
		cfg := atomscout.Config{
			Name:     strings.TrimSpace(entry.Name),
			Format:   firstNonEmpty(entry.Format, "atom"),
			SpanName: firstNonEmpty(entry.SpanName, discovery.ScoutDiscoverSpanName("atom", entry.Name)),
		}.Normalize()
		if err := cfg.Validate(); err != nil {
			return err
		}

		hosts, err := normalizeHosts(entry.Hosts)
		if err != nil {
			return fmt.Errorf("%s: %w", cfg.Name, err)
		}
		if enabled {
			if err := r.registerHosts("atom", cfg.Name, hosts); err != nil {
				return err
			}
		}

		r.atom[cfg.Name] = FeedSpec{
			Enabled: enabled,
			Hosts:   hosts,
			Config:  cfg,
		}
	}

	return nil
}

func (r *Repository) loadCustom(section CustomSection) error {
	for _, entry := range section.Scouts {
		enabled := resolveEnabled(section.Defaults.Enabled, entry.Enabled)
		name := strings.TrimSpace(entry.Name)

		switch name {
		case "yahoo":
			cfg := yahooscout.Config{
				Name:     name,
				Format:   firstNonEmpty(entry.Format, "custom"),
				SpanName: firstNonEmpty(entry.SpanName, discovery.ScoutDiscoverSpanName("custom", name)),
				Headers:  mergeHeaders(nil, entry.Headers),
			}.Normalize()
			if err := cfg.Validate(); err != nil {
				return err
			}

			hosts, err := normalizeHosts(entry.Hosts)
			if err != nil {
				return fmt.Errorf("%s: %w", cfg.Name, err)
			}
			if enabled {
				if err := r.registerHosts("custom", cfg.Name, hosts); err != nil {
					return err
				}
			}

			r.custom[cfg.Name] = CustomSpec{
				Enabled: enabled,
				Hosts:   hosts,
				Config:  cfg,
			}
		default:
			return fmt.Errorf("%w: %s", ErrUnknownCustomScout, name)
		}
	}

	return nil
}

func (r *Repository) registerHosts(kind, name string, hosts []string) error {
	for _, host := range hosts {
		if current, ok := r.byHost[host]; ok {
			return fmt.Errorf("%w: %s (%s conflicts with %s)", ErrDuplicateScoutHost, host, current, kind+"/"+name)
		}
		r.byHost[host] = kind + "/" + name
	}
	return nil
}

func resolveEnabled(def, value *bool) bool {
	if value != nil {
		return *value
	}
	if def != nil {
		return *def
	}
	return true
}

func mergeHeaders(base, override map[string]string) map[string]string {
	out := htmlscout.CloneHeaders(base)
	if out == nil {
		out = make(map[string]string)
	}
	for key, value := range htmlscout.CloneHeaders(override) {
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeHosts(hosts []string) ([]string, error) {
	out := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" {
			continue
		}
		out = append(out, host)
	}
	if len(out) == 0 {
		return nil, ErrScoutHostsEmpty
	}
	return out, nil
}

func firstNonEmpty(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v != "" {
		return v
	}
	return strings.TrimSpace(fallback)
}
