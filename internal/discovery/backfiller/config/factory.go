package config

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"

	"github.com/ChiaYuChang/prism/internal/discovery/backfiller"
	scoutconfig "github.com/ChiaYuChang/prism/internal/discovery/scout/config"
	discoverysink "github.com/ChiaYuChang/prism/internal/discovery/sink"
	"go.opentelemetry.io/otel/trace"
)

func BuildPager(logger *slog.Logger, tracer trace.Tracer, spec SourceConfig) (backfiller.Pager, error) {
	switch spec.Pager.Type {
	case "index":
		mode := backfiller.PagerMode(spec.Pager.Mode)
		if mode != backfiller.PageModeIndex {
			return nil, fmt.Errorf("%w: pager mode %s", backfiller.ErrNotImplemented, mode)
		}

		return backfiller.NewIndexPager(logger, tracer, backfiller.IndexPagerConfig{
			BaseURL:     spec.BaseURL,
			URLTemplate: spec.Pager.URLTemplate,
			First:       spec.Pager.First,
			Step:        spec.Pager.Step,
			Mode:        mode,
			Params:      spec.Pager.Params,
		})
	default:
		return nil, fmt.Errorf("unknown pager type: %s", spec.Pager.Type)
	}
}

func BuildBackfiller(
	spec SourceConfig,
	scoutRepo *scoutconfig.Repository,
	logger *slog.Logger,
	tracer trace.Tracer,
	client *http.Client,
	sink discoverysink.CandidateSink,
) (*backfiller.Backfiller, error) {
	scout, err := scoutconfig.BuildScoutByName(scoutRepo, spec.Name, logger, tracer, client)
	if err != nil {
		return nil, fmt.Errorf("build scout %s: %w", spec.Name, err)
	}

	pager, err := BuildPager(logger, tracer, spec)
	if err != nil {
		return nil, fmt.Errorf("build pager for %s: %w", spec.Name, err)
	}

	return backfiller.New(logger, tracer, scout, pager, sink, spec.SourceID, spec.Timeout)
}

func ConfirmSourceAgainstScout(spec SourceConfig, repo *scoutconfig.Repository) error {
	if repo == nil {
		return fmt.Errorf("scout config repo is nil")
	}

	u, err := url.Parse(spec.BaseURL)
	if err != nil {
		return fmt.Errorf("parse base_url: %w", err)
	}
	if u.Host == "" {
		return fmt.Errorf("base_url has empty host")
	}

	var hosts []string
	switch spec.Format {
	case "html":
		scoutSpec, ok := repo.HTML(spec.Name)
		if !ok || !scoutSpec.Enabled {
			return fmt.Errorf("html scout %q not found or disabled", spec.Name)
		}
		hosts = scoutSpec.Hosts
	case "rss":
		scoutSpec, ok := repo.RSS(spec.Name)
		if !ok || !scoutSpec.Enabled {
			return fmt.Errorf("rss scout %q not found or disabled", spec.Name)
		}
		hosts = scoutSpec.Hosts
	case "atom":
		scoutSpec, ok := repo.Atom(spec.Name)
		if !ok || !scoutSpec.Enabled {
			return fmt.Errorf("atom scout %q not found or disabled", spec.Name)
		}
		hosts = scoutSpec.Hosts
	case "custom":
		scoutSpec, ok := repo.Custom(spec.Name)
		if !ok || !scoutSpec.Enabled {
			return fmt.Errorf("custom scout %q not found or disabled", spec.Name)
		}
		hosts = scoutSpec.Hosts
	default:
		return fmt.Errorf("unsupported source format: %s", spec.Format)
	}

	if !slices.Contains(hosts, u.Host) {
		return fmt.Errorf("base_url host %q does not match scout hosts %v", u.Host, hosts)
	}
	return nil
}
