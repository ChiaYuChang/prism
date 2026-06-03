package config

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/ChiaYuChang/prism/internal/appconfig"
	"github.com/ChiaYuChang/prism/internal/discovery"
	prismlogger "github.com/ChiaYuChang/prism/pkg/logger"
)

// Config describes keyword-search targets and external search providers.
type Config struct {
	Targets  map[string]TargetConfig `json:"targets"  yaml:"targets"  mapstructure:"targets"`
	Provider ProviderConfig          `json:"provider" yaml:"provider" mapstructure:"provider"`
}

// TargetConfig describes a real media source that receives search candidates.
type TargetConfig struct {
	Enable     bool   `json:"enable"      yaml:"enable"      mapstructure:"enable"`
	SourceAbbr string `json:"source_abbr" yaml:"source_abbr" mapstructure:"source_abbr"`
	URL        string `json:"url"         yaml:"url"         mapstructure:"url"`
	Site       string `json:"site"        yaml:"site"        mapstructure:"site"`
}

// ProviderConfig groups all supported search provider configs.
type ProviderConfig struct {
	Brave     BraveConfig     `json:"brave"      yaml:"brave"      mapstructure:"brave"`
	GoogleCSE GoogleCSEConfig `json:"google-cse" yaml:"google-cse" mapstructure:"google-cse"`
	SerpAPI   SerpAPIConfig   `json:"serpapi"    yaml:"serpapi"    mapstructure:"serpapi"`
}

// BraveConfig configures Brave News Search.
type BraveConfig struct {
	Enable               bool   `json:"enable"                 yaml:"enable"                 mapstructure:"enable"`
	APIKey               string `json:"api_key"                yaml:"api_key"                mapstructure:"api_key"`
	APIKeyFile           string `json:"api_key_file"           yaml:"api_key_file"           mapstructure:"api_key_file"`
	Count                int    `json:"count"                  yaml:"count"                  mapstructure:"count"`
	Offset               int    `json:"offset"                 yaml:"offset"                 mapstructure:"offset"`
	SearchLang           string `json:"search_lang"            yaml:"search_lang"            mapstructure:"search_lang"`
	UILang               string `json:"ui_lang"                yaml:"ui_lang"                mapstructure:"ui_lang"`
	Country              string `json:"country"                yaml:"country"                mapstructure:"country"`
	Freshness            string `json:"freshness"              yaml:"freshness"              mapstructure:"freshness"`
	SafeSearch           string `json:"safesearch"             yaml:"safesearch"             mapstructure:"safesearch"`
	Spellcheck           *bool  `json:"spellcheck"             yaml:"spellcheck"             mapstructure:"spellcheck"`
	ExtraSnippets        string `json:"extra_snippets"         yaml:"extra_snippets"         mapstructure:"extra_snippets"`
	Goggles              string `json:"goggles"                yaml:"goggles"                mapstructure:"goggles"`
	IncludeFetchMetadata *bool  `json:"include_fetch_metadata" yaml:"include_fetch_metadata" mapstructure:"include_fetch_metadata"`
	Operators            *bool  `json:"operators"              yaml:"operators"              mapstructure:"operators"`
	APIVersion           string `json:"api_version"            yaml:"api_version"            mapstructure:"api_version"`
	CacheControl         string `json:"cache_control"          yaml:"cache_control"          mapstructure:"cache_control"`
	UserAgent            string `json:"user_agent"             yaml:"user_agent"             mapstructure:"user_agent"`
}

// GoogleCSEConfig configures Google Custom Search JSON API.
type GoogleCSEConfig struct {
	Enable           bool   `json:"enable"             yaml:"enable"             mapstructure:"enable"`
	APIKey           string `json:"api_key"            yaml:"api_key"            mapstructure:"api_key"`
	APIKeyFile       string `json:"api_key_file"       yaml:"api_key_file"       mapstructure:"api_key_file"`
	CX               string `json:"cx"                 yaml:"cx"                 mapstructure:"cx"`
	Count            int    `json:"count"              yaml:"count"              mapstructure:"count"`
	Language         string `json:"language"           yaml:"language"           mapstructure:"language"`
	Country          string `json:"country"            yaml:"country"            mapstructure:"country"`
	GeoLocation      string `json:"geo_location"       yaml:"geo_location"       mapstructure:"geo_location"`
	InterfaceLang    string `json:"interface_lang"     yaml:"interface_lang"     mapstructure:"interface_lang"`
	DateRestrict     string `json:"date_restrict"      yaml:"date_restrict"      mapstructure:"date_restrict"`
	ExactTerms       string `json:"exact_terms"        yaml:"exact_terms"        mapstructure:"exact_terms"`
	ExcludeTerms     string `json:"exclude_terms"      yaml:"exclude_terms"      mapstructure:"exclude_terms"`
	OrTerms          string `json:"or_terms"           yaml:"or_terms"           mapstructure:"or_terms"`
	HighQualityTerms string `json:"high_quality_terms" yaml:"high_quality_terms" mapstructure:"high_quality_terms"`
	Safe             string `json:"safe"               yaml:"safe"               mapstructure:"safe"`
	Sort             string `json:"sort"               yaml:"sort"               mapstructure:"sort"`
	Filter           string `json:"filter"             yaml:"filter"             mapstructure:"filter"`
	ChineseSearch    string `json:"chinese_search"     yaml:"chinese_search"     mapstructure:"chinese_search"`
}

// SerpAPIConfig configures shared SerpAPI credentials and per-engine options.
type SerpAPIConfig struct {
	Enable     bool              `json:"enable"          yaml:"enable"          mapstructure:"enable"`
	APIKey     string            `json:"api_key"         yaml:"api_key"         mapstructure:"api_key"`
	APIKeyFile string            `json:"api_key_file"    yaml:"api_key_file"    mapstructure:"api_key_file"`
	NoCache    *bool             `json:"no_cache"        yaml:"no_cache"        mapstructure:"no_cache"`
	GoogleNews SerpAPIGoogleNews `json:"google_news"     yaml:"google_news"     mapstructure:"google_news"`
	DuckDuckGo SerpAPIDuckDuckGo `json:"duckduckgo_news" yaml:"duckduckgo_news" mapstructure:"duckduckgo_news"`
	BingNews   SerpAPIBingNews   `json:"bing_news"       yaml:"bing_news"       mapstructure:"bing_news"`
}

type SerpAPIGoogleNews struct {
	Enable bool                               `json:"enable" yaml:"enable" mapstructure:"enable"`
	Params map[string]SerpAPIGoogleNewsParams `json:"params" yaml:"params" mapstructure:"params"`
}

type SerpAPIGoogleNewsParams struct {
	Enable           bool   `json:"enable"            yaml:"enable"            mapstructure:"enable"`
	Geolocation      string `json:"gl"                yaml:"geolocation"       mapstructure:"geolocation"`
	HostLanguage     string `json:"hl"                yaml:"host_language"     mapstructure:"host_language"`
	TopicToken       string `json:"topic_token"       yaml:"topic_token"       mapstructure:"topic_token"`
	SectionToken     string `json:"section_token"     yaml:"section_token"     mapstructure:"section_token"`
	StoryToken       string `json:"story_token"       yaml:"story_token"       mapstructure:"story_token"`
	PublicationToken string `json:"publication_token" yaml:"publication_token" mapstructure:"publication_token"`
	SortOrder        int    `json:"so"                yaml:"sort_order"        mapstructure:"sort_order"`
}

type SerpAPIBingNews struct {
	Enable bool                         `json:"enable" yaml:"enable" mapstructure:"enable"`
	Params map[string]SerpAPIBingParams `json:"params" yaml:"params" mapstructure:"params"`
}

type SerpAPIBingParams struct {
	Enable          bool   `json:"enable"     yaml:"enable"            mapstructure:"enable"`
	MarketCode      string `json:"mkt"        yaml:"market_code"       mapstructure:"market_code"`
	CountryCode     string `json:"cc"         yaml:"country_code"      mapstructure:"country_code"`
	PaginationFirst int    `json:"first"      yaml:"pagination_first"  mapstructure:"pagination_first"`
	PaginationCount int    `json:"count"      yaml:"pagination_count"  mapstructure:"pagination_count"`
	QueryFilter     string `json:"qft"        yaml:"query_filter"      mapstructure:"query_filter"`
	SafeSearch      string `json:"safeSearch" yaml:"safe_search"       mapstructure:"safe_search"`
}

type SerpAPIDuckDuckGo struct {
	Enable bool                                   `json:"enable" yaml:"enable" mapstructure:"enable"`
	Params map[string]SerpAPIDuckDuckGoNewsParams `json:"params" yaml:"params" mapstructure:"params"`
}

type SerpAPIDuckDuckGoNewsParams struct {
	Enable          bool   `json:"enable" yaml:"enable"            mapstructure:"enable"`
	RegionCode      string `json:"kl"     yaml:"region_code"       mapstructure:"region_code"`
	SafeSearch      int    `json:"safe"   yaml:"safe_search"       mapstructure:"safe_search"`
	DateFilter      string `json:"df"     yaml:"date_filter"       mapstructure:"date_filter"`
	PaginationStart int    `json:"start"  yaml:"pagination_start"  mapstructure:"pagination_start"`
	PaginationCount int    `json:"count"  yaml:"pagination_count"  mapstructure:"pagination_count"`
}

// ResolveSecrets loads provider API keys from files when configured.
func (c *Config) ResolveSecrets(logger *slog.Logger) error {
	if err := resolveAPIKey(&c.Provider.Brave.APIKey, c.Provider.Brave.APIKeyFile, "brave", logger); err != nil {
		return err
	}
	if err := resolveAPIKey(&c.Provider.GoogleCSE.APIKey, c.Provider.GoogleCSE.APIKeyFile, "google-cse", logger); err != nil {
		return err
	}
	if err := resolveAPIKey(&c.Provider.SerpAPI.APIKey, c.Provider.SerpAPI.APIKeyFile, "serpapi", logger); err != nil {
		return err
	}
	return nil
}

// EnabledTargets returns enabled search targets as PlannerTarget values.
func (c Config) EnabledTargets() []discovery.PlannerTarget {
	targets := make([]discovery.PlannerTarget, 0, len(c.Targets))
	for _, target := range c.Targets {
		if !target.Enable {
			continue
		}
		targets = append(targets, discovery.PlannerTarget{
			SourceAbbr: strings.TrimSpace(target.SourceAbbr),
			URL:        strings.TrimSpace(target.URL),
			Site:       strings.TrimSpace(target.Site),
		})
	}
	return targets
}

func resolveAPIKey(key *string, keyFile, provider string, logger *slog.Logger) error {
	if key == nil {
		return nil
	}
	if strings.TrimSpace(keyFile) != "" {
		v, err := appconfig.LoadFromFile(keyFile)
		if err != nil {
			return fmt.Errorf("%s api_key_file: %w", provider, err)
		}
		*key = v
		return nil
	}
	if strings.TrimSpace(*key) != "" && logger != nil {
		logger.Warn("search provider uses inline api_key; prefer api_key_file for local secrets",
			slog.String("provider", provider),
			slog.String("api_key", prismlogger.SecretMask(*key)),
		)
	}
	return nil
}
