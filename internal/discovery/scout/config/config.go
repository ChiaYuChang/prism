package config

import (
	htmlscout "github.com/ChiaYuChang/prism/internal/discovery/scout/html"
)

const CurrentVersion = 1

type Config struct {
	Version int         `yaml:"version" json:"version"`
	Scout   ScoutConfig `yaml:"scout"   json:"scout"`
}

type ScoutConfig struct {
	HTML   HTMLSection   `yaml:"html"   json:"html"`
	RSS    FeedSection   `yaml:"rss"    json:"rss"`
	Atom   FeedSection   `yaml:"atom"   json:"atom"`
	Custom CustomSection `yaml:"custom" json:"custom"`
}

type HTMLSection struct {
	Defaults HTMLDefaults      `yaml:"defaults" json:"defaults"`
	Scouts   []HTMLScoutConfig `yaml:"scouts"   json:"scouts"`
}

type FeedSection struct {
	Defaults FeedDefaults      `yaml:"defaults" json:"defaults"`
	Scouts   []FeedScoutConfig `yaml:"scouts"   json:"scouts"`
}

type CustomSection struct {
	Defaults CustomDefaults      `yaml:"defaults" json:"defaults"`
	Scouts   []CustomScoutConfig `yaml:"scouts"   json:"scouts"`
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
	Name     string                 `yaml:"name"      json:"name"`
	Format   string                 `yaml:"format"    json:"format"`
	SpanName string                 `yaml:"span_name" json:"span_name"`
	Hosts    []string               `yaml:"hosts"     json:"hosts"`
	Headers  map[string]string      `yaml:"headers"   json:"headers"`
	Rules    []htmlscout.RuleConfig `yaml:"rules"     json:"rules"`
}

type FeedScoutConfig struct {
	Enabled  *bool             `yaml:"enabled"   json:"enabled"`
	Name     string            `yaml:"name"      json:"name"`
	Format   string            `yaml:"format"    json:"format"`
	SpanName string            `yaml:"span_name" json:"span_name"`
	Hosts    []string          `yaml:"hosts"     json:"hosts"`
	Headers  map[string]string `yaml:"headers"   json:"headers"`
}

type CustomScoutConfig struct {
	Enabled  *bool             `yaml:"enabled"   json:"enabled"`
	Name     string            `yaml:"name"      json:"name"`
	Format   string            `yaml:"format"    json:"format"`
	SpanName string            `yaml:"span_name" json:"span_name"`
	Hosts    []string          `yaml:"hosts"     json:"hosts"`
	Headers  map[string]string `yaml:"headers"   json:"headers"`
}
