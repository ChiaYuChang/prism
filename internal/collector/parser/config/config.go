package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/collector/parser/html"
	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Version  int                     `yaml:"version"  json:"version,omitempty"`
	Fallback FallbackConfig          `yaml:"fallback" json:"fallback,omitempty"`
	Parsers  map[string]ParserConfig `yaml:"parsers"  json:"parsers,omitempty"`
}

// FallbackConfig is the global fallback section. The LLM block is decoded
// directly from parsers.yaml so the operator sees the provider choice in
// the same file as parser rules. The actual API key should arrive via
// LLM.KeyFile (path to a mounted secret); LLM.Key inline is supported but
// discouraged for production deploys.
//
// Future per-input-type sub-sections (json/xml/rss with custom prompts) are
// a planned extension; v1 assumes HTML-shape text input.
type FallbackConfig struct {
	Enable bool                `yaml:"enable"      json:"enable,omitempty"`
	LLM    appconfig.LLMConfig `yaml:"llm"         json:"llm,omitempty"`

	// PromptFile is the path to the system-instruction text passed to the
	// LLM (typically assets/prompts/collector/article_parser.md, which is
	// baked into worker images at /app/assets/prompts/collector/...). Kept
	// out of the binary so operators can iterate on extraction quality
	// without rebuilding. Required when Enable=true.
	PromptFile string `yaml:"prompt_file" json:"prompt_file,omitempty"`
}

type ParserConfig struct {
	Enabled     *bool           `yaml:"enabled"      json:"enabled,omitempty"`
	JSONLD      bool            `yaml:"jsonld"       json:"jsonld,omitempty"`
	DateLayouts []string        `yaml:"date_layouts" json:"date_layouts,omitempty"`
	HTML        html.RuleConfig `yaml:"html"         json:"html,omitempty"`
}

func LoadConfig(path string) (cfg Config, err error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open parser config: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close parser config: %w", cerr)
		}
	}()

	if derr := yaml.NewDecoder(f).Decode(&cfg); derr != nil {
		return Config{}, fmt.Errorf("decode parser config: %w", derr)
	}

	if cfg.Fallback.Enable {
		if cfg.Fallback.PromptFile == "" {
			return Config{}, fmt.Errorf("fallback.prompt_file is required when fallback.enable=true")
		}
		if rerr := cfg.Fallback.LLM.ResolveSecrets(); rerr != nil {
			return Config{}, fmt.Errorf("resolve fallback llm secrets: %w", rerr)
		}
		if verr := validator.New().Struct(&cfg.Fallback.LLM); verr != nil {
			return Config{}, fmt.Errorf("fallback llm config invalid: %w", verr)
		}
	}

	return cfg, nil
}

// LoadFallbackPrompt reads the system-instruction file pointed to by
// FallbackConfig.PromptFile. Trim trailing whitespace so an editor's
// auto-newline doesn't confuse the LLM. Errors if PromptFile is empty.
func LoadFallbackPrompt(cfg FallbackConfig) (string, error) {
	if cfg.PromptFile == "" {
		return "", fmt.Errorf("fallback.prompt_file is empty")
	}
	body, err := os.ReadFile(cfg.PromptFile)
	if err != nil {
		return "", fmt.Errorf("read fallback prompt: %w", err)
	}
	return strings.TrimRight(string(body), " \t\n\r"), nil
}
