package config

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ChiaYuChang/prism/internal/discovery"
	rootscout "github.com/ChiaYuChang/prism/internal/discovery/scout"
	atomscout "github.com/ChiaYuChang/prism/internal/discovery/scout/atom"
	yahooscout "github.com/ChiaYuChang/prism/internal/discovery/scout/custom/yahoo"
	htmlscout "github.com/ChiaYuChang/prism/internal/discovery/scout/html"
	rssscout "github.com/ChiaYuChang/prism/internal/discovery/scout/rss"
	"go.opentelemetry.io/otel/trace"
)

func BuildRegistry(repo *Repository, logger *slog.Logger, tracer trace.Tracer, client *http.Client) (*rootscout.Registry, error) {
	if repo == nil {
		return nil, fmt.Errorf("scout config repo is nil")
	}

	scouts := make(map[string]discovery.Scout)

	for _, spec := range repo.html {
		if !spec.Enabled {
			continue
		}
		scout, err := htmlscout.New(logger, tracer, client, spec.Config)
		if err != nil {
			return nil, fmt.Errorf("build html scout %s: %w", spec.Config.Name, err)
		}
		for _, host := range spec.Hosts {
			scouts[host] = scout
		}
	}

	for _, spec := range repo.rss {
		if !spec.Enabled {
			continue
		}
		cfg := spec.Config.(rssscout.Config)
		scout, err := rssscout.New(logger, tracer, client, cfg)
		if err != nil {
			return nil, fmt.Errorf("build rss scout %s: %w", cfg.Name, err)
		}
		for _, host := range spec.Hosts {
			scouts[host] = scout
		}
	}

	for _, spec := range repo.atom {
		if !spec.Enabled {
			continue
		}
		cfg := spec.Config.(atomscout.Config)
		scout, err := atomscout.New(logger, tracer, client, cfg)
		if err != nil {
			return nil, fmt.Errorf("build atom scout %s: %w", cfg.Name, err)
		}
		for _, host := range spec.Hosts {
			scouts[host] = scout
		}
	}

	for _, spec := range repo.custom {
		if !spec.Enabled {
			continue
		}
		switch cfg := spec.Config.(type) {
		case yahooscout.Config:
			scout, err := yahooscout.New(logger, tracer, client, cfg)
			if err != nil {
				return nil, fmt.Errorf("build custom scout %s: %w", cfg.Name, err)
			}
			for _, host := range spec.Hosts {
				scouts[host] = scout
			}
		default:
			return nil, fmt.Errorf("%w: %T", ErrUnknownCustomScout, spec.Config)
		}
	}

	return rootscout.NewRegistry(logger, tracer, scouts)
}
