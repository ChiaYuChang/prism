//go:build smoke

package search

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/dev"
	"github.com/ChiaYuChang/prism/internal/discovery/search/brave"
	searchconfig "github.com/ChiaYuChang/prism/internal/discovery/search/config"
	"github.com/ChiaYuChang/prism/internal/discovery/search/googlecse"
	"github.com/ChiaYuChang/prism/internal/discovery/search/serpapi"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	smokeQuery      = "computex"
	smokeSite       = "tw.news.yahoo.com"
	smokeCaptureDir = "testdata/real/search-smoke"
)

func TestSearchProvidersSmokeCapture(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := dev.WrapClient(&http.Client{Timeout: 15 * time.Second}, smokeCaptureDir, logger)

	root := repoRoot(t)
	cfg := loadLocalSearchConfig(t, root, logger)
	braveCfg := cfg.Provider.Brave
	if braveCfg.Enable && braveCfg.APIKey != "" {
		t.Run("brave", func(t *testing.T) {
			provider := brave.NewClient(client, braveCfg.APIKey, brave.Options{
				Count:      smokeCount(braveCfg.Count),
				SearchLang: braveCfg.SearchLang,
				UILang:     braveCfg.UILang,
				Country:    braveCfg.Country,
				Freshness:  braveCfg.Freshness,
			})

			candidates, err := provider.DiscoverNews(ctx, smokeQuery, smokeSite)
			require.NoError(t, err)
			t.Logf("brave returned %d candidates", len(candidates))
		})
	}
	if !braveCfg.Enable || braveCfg.APIKey == "" {
		t.Log("brave smoke skipped: enable provider and set api_key_file/api_key in env/local/search.local.yaml")
	}

	googleCfg := cfg.Provider.GoogleCSE
	if googleCfg.Enable && googleCfg.APIKey != "" && googleCfg.CX != "" {
		t.Run("google-cse", func(t *testing.T) {
			provider := googlecse.NewClient(client, googleCfg.APIKey, googleCfg.CX, googlecse.Options{
				Count:         smokeCount(googleCfg.Count),
				Language:      googleCfg.Language,
				Country:       googleCfg.Country,
				GeoLocation:   googleCfg.GeoLocation,
				InterfaceLang: googleCfg.InterfaceLang,
				DateRestrict:  googleCfg.DateRestrict,
			})

			candidates, err := provider.DiscoverNews(ctx, smokeQuery, smokeSite)
			require.NoError(t, err)
			t.Logf("google-cse returned %d candidates", len(candidates))
		})
	}
	if !googleCfg.Enable || googleCfg.APIKey == "" || googleCfg.CX == "" {
		t.Log("google-cse smoke skipped: enable provider and set api_key_file/api_key plus cx in env/local/search.local.yaml")
	}

	serpCfg := cfg.Provider.SerpAPI
	runSerpAPIGoogleNewsSmoke(t, ctx, client, serpCfg)
	runSerpAPIDuckDuckGoNewsSmoke(t, ctx, client, serpCfg)
	runSerpAPIBingNewsSmoke(t, ctx, client, serpCfg)
	if noSerpAPISmokeProviders(cfg) {
		t.Log("serpapi smoke skipped: enable provider and set api_key_file/api_key in env/local/search.local.yaml")
	}

	if (!braveCfg.Enable || braveCfg.APIKey == "") && (!googleCfg.Enable || googleCfg.APIKey == "" || googleCfg.CX == "") && noSerpAPISmokeProviders(cfg) {
		t.Skip("no search provider smoke credentials configured")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func loadLocalSearchConfig(t *testing.T, root string, logger *slog.Logger) searchconfig.Config {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "env", "local", "search.local.yaml"))
	require.NoError(t, err)

	var wrapper struct {
		Search searchconfig.Config `yaml:"search"`
	}
	require.NoError(t, yaml.Unmarshal(body, &wrapper))
	makeKeyFilesAbsolute(root, &wrapper.Search)
	require.NoError(t, wrapper.Search.ResolveSecrets(logger))
	return wrapper.Search
}

func makeKeyFilesAbsolute(root string, cfg *searchconfig.Config) {
	cfg.Provider.Brave.APIKeyFile = absFromRoot(root, cfg.Provider.Brave.APIKeyFile)
	cfg.Provider.GoogleCSE.APIKeyFile = absFromRoot(root, cfg.Provider.GoogleCSE.APIKeyFile)
	cfg.Provider.SerpAPI.APIKeyFile = absFromRoot(root, cfg.Provider.SerpAPI.APIKeyFile)
}

func absFromRoot(root, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func smokeCount(count int) int {
	if count <= 0 || count > 5 {
		return 5
	}
	return count
}

func runSerpAPIGoogleNewsSmoke(t *testing.T, ctx context.Context, client *http.Client, cfg searchconfig.SerpAPIConfig) {
	engine := cfg.GoogleNews
	if !cfg.Enable || !engine.Enable || cfg.APIKey == "" {
		return
	}
	for name, params := range engine.Params {
		if !params.Enable {
			continue
		}
		t.Run("serpapi-google-news-"+name, func(t *testing.T) {
			provider := serpapi.NewClient(client, cfg.APIKey, serpapi.Options{
				Engine:    "google_news",
				Country:   params.Geolocation,
				Language:  params.HostLanguage,
				SortOrder: params.SortOrder,
				NoCache:   cfg.NoCache,
			})

			candidates, err := provider.DiscoverNews(ctx, smokeQuery, smokeSite)
			require.NoError(t, err)
			t.Logf("serpapi-google-news-%s returned %d candidates", name, len(candidates))
		})
	}
}

func runSerpAPIDuckDuckGoNewsSmoke(t *testing.T, ctx context.Context, client *http.Client, cfg searchconfig.SerpAPIConfig) {
	engine := cfg.DuckDuckGo
	if !cfg.Enable || !engine.Enable || cfg.APIKey == "" {
		return
	}
	for name, params := range engine.Params {
		if !params.Enable {
			continue
		}
		t.Run("serpapi-duckduckgo-news-"+name, func(t *testing.T) {
			provider := serpapi.NewClient(client, cfg.APIKey, serpapi.Options{
				Engine:     "duckduckgo_news",
				Region:     params.RegionCode,
				Safe:       fmt.Sprintf("%d", params.SafeSearch),
				DateFilter: params.DateFilter,
				Start:      params.PaginationStart,
				MaxResults: params.PaginationCount,
				NoCache:    cfg.NoCache,
			})

			candidates, err := provider.DiscoverNews(ctx, smokeQuery, smokeSite)
			require.NoError(t, err)
			t.Logf("serpapi-duckduckgo-news-%s returned %d candidates", name, len(candidates))
		})
	}
}

func runSerpAPIBingNewsSmoke(t *testing.T, ctx context.Context, client *http.Client, cfg searchconfig.SerpAPIConfig) {
	engine := cfg.BingNews
	if !cfg.Enable || !engine.Enable || cfg.APIKey == "" {
		return
	}
	for name, params := range engine.Params {
		if !params.Enable {
			continue
		}
		t.Run("serpapi-bing-news-"+name, func(t *testing.T) {
			provider := serpapi.NewClient(client, cfg.APIKey, serpapi.Options{
				Engine:     "bing_news",
				Country:    params.CountryCode,
				Language:   params.MarketCode,
				Safe:       params.SafeSearch,
				Start:      params.PaginationFirst,
				MaxResults: params.PaginationCount,
				Filter:     params.QueryFilter,
				NoCache:    cfg.NoCache,
			})

			candidates, err := provider.DiscoverNews(ctx, smokeQuery, smokeSite)
			require.NoError(t, err)
			t.Logf("serpapi-bing-news-%s returned %d candidates", name, len(candidates))
		})
	}
}

func noSerpAPISmokeProviders(cfg searchconfig.Config) bool {
	serp := cfg.Provider.SerpAPI
	return !serp.Enable || serp.APIKey == "" || (!hasEnabledGoogleNewsParams(serp.GoogleNews) &&
		!hasEnabledDuckDuckGoParams(serp.DuckDuckGo) &&
		!hasEnabledBingNewsParams(serp.BingNews))
}

func hasEnabledGoogleNewsParams(engine searchconfig.SerpAPIGoogleNews) bool {
	if !engine.Enable {
		return false
	}
	for _, params := range engine.Params {
		if params.Enable {
			return true
		}
	}
	return false
}

func hasEnabledDuckDuckGoParams(engine searchconfig.SerpAPIDuckDuckGo) bool {
	if !engine.Enable {
		return false
	}
	for _, params := range engine.Params {
		if params.Enable {
			return true
		}
	}
	return false
}

func hasEnabledBingNewsParams(engine searchconfig.SerpAPIBingNews) bool {
	if !engine.Enable {
		return false
	}
	for _, params := range engine.Params {
		if params.Enable {
			return true
		}
	}
	return false
}
