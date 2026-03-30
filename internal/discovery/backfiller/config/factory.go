package config

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ChiaYuChang/prism/internal/discovery/backfiller"
	scoutconfig "github.com/ChiaYuChang/prism/internal/discovery/scout/config"
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
			URLTemplate: spec.Pager.URLTemplate,
			First:       spec.Pager.First,
			Step:        spec.Pager.Step,
			Mode:        mode,
			Params:      spec.Pager.Params,
			OmitFirst:   spec.Pager.OmitFirst,
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
	sink backfiller.Sink,
) (*backfiller.Backfiller, error) {
	scout, err := scoutconfig.BuildScoutByName(scoutRepo, spec.Name, logger, tracer, client)
	if err != nil {
		return nil, fmt.Errorf("build scout %s: %w", spec.Name, err)
	}

	pager, err := BuildPager(logger, tracer, spec)
	if err != nil {
		return nil, fmt.Errorf("build pager for %s: %w", spec.Name, err)
	}

	return backfiller.New(logger, tracer, scout, pager, sink, spec.Timeout)
}
