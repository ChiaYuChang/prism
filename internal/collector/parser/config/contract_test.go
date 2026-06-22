package config_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector/parser/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
	"gopkg.in/yaml.v3"
)

// This is a CONFIG CONTRACT test, not a parser-logic test.
//
// Parser logic (selector engine, @attr, date-layout try-order) is pinned by
// parser/html and parser/jsonld unit tests with hard-coded expected values.
// Those would still pass if parsers.yaml went stale — because they build their
// own RuleConfig in test code.
//
// This test wires the REAL parsers.yaml through BuildRegistry → Registry.Parse
// against synthetic fixtures in testdata/synthetic. Optional local real captures
// under testdata/real run when present and are skipped otherwise.
//
// The assertions are intentionally loose (non-empty / non-zero / min length)
// because *content correctness* is the job of the per-parser unit tests.
// What this guards is: "does this host's config still extract SOMETHING from
// a known-good page?"

// hostFixtures maps hosts in parsers.yaml to mirrored fixture names under
// testdata/{synthetic,real}/collector/parser.
// When adding a host to parsers.yaml, add at least one synthetic fixture entry here.
// A host without a fixture skips with a clear message — CI won't fail, but
// the skip is visible in test output so drift is still observable.
var hostFixtures = map[string]struct {
	fixture string
	url     string
}{
	"www.dpp.org.tw": {"dpp_11545.html", "https://www.dpp.org.tw/media/contents/11545"},
	"www.tpp.org.tw": {"tpp_4530.html", "https://www.tpp.org.tw/newsdetail/4530"},
	"www.kmt.org.tw": {"kmt_blog-post_20.html", "https://www.kmt.org.tw/2026/04/blog-post_20.html"},
	// tw.news.yahoo.com: no parser fixture yet; discovery uses a custom scout.
}

func TestParsersConfig_ContractEachHost(t *testing.T) {
	runParsersConfigContract(t, parserFixtureRoot("synthetic"), true)
}

func TestParsersConfig_RealContractEachHost(t *testing.T) {
	runParsersConfigContract(t, parserFixtureRoot("real"), false)
}

func runParsersConfigContract(t *testing.T, fixtureRoot string, required bool) {
	t.Helper()
	if _, err := os.Stat(fixtureRoot); err != nil {
		if !required && errors.Is(err, os.ErrNotExist) {
			t.Skipf("real parser fixtures not found at %s; local captures are optional", fixtureRoot)
		}
		require.NoError(t, err)
	}

	body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "configs", "worker", "collector", "parsers.yaml"))
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, yaml.Unmarshal(body, &cfg))
	require.NotEmpty(t, cfg.Parsers, "parsers.yaml has no hosts")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")

	registry, err := config.BuildRegistry(cfg, logger, tracer, nil)
	require.NoError(t, err)

	for host, pCfg := range cfg.Parsers {
		if pCfg.Enabled != nil && !*pCfg.Enabled {
			continue
		}
		t.Run(host, func(t *testing.T) {
			fx, ok := hostFixtures[host]
			if !ok {
				t.Skipf("no fixture for %s — add one to hostFixtures in contract_test.go to guard against selector drift", host)
			}

			data, err := os.ReadFile(filepath.Join(fixtureRoot, fx.fixture))
			if !required && errors.Is(err, os.ErrNotExist) {
				t.Skipf("real fixture %s not found under %s", fx.fixture, fixtureRoot)
			}
			require.NoError(t, err)

			article, err := registry.Parse(context.Background(), fx.url, string(data))
			require.NoError(t, err)
			require.NotNil(t, article)

			assert.NotEmptyf(t, article.Title,
				"title empty for %s — title selector may have drifted from the live site", host)
			assert.Falsef(t, article.PublishedAt.IsZero(),
				"published_at zero for %s — date selector or date_layouts may have drifted", host)
			assert.GreaterOrEqualf(t, len(article.Content), 100,
				"content for %s is only %d chars — content selector may have drifted", host, len(article.Content))
		})
	}
}

func parserFixtureRoot(kind string) string {
	return filepath.Join("..", "..", "..", "..", "testdata", kind, "collector", "parser")
}
