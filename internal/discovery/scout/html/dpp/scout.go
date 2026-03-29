package dpp

import (
	"log/slog"
	"net/http"

	htmlscout "github.com/ChiaYuChang/prism/internal/discovery/scout/html"
	"go.opentelemetry.io/otel/trace"
)

const (
	scoutDateLayout = "2006-01-02"
	scoutFormat     = "html"
	scoutName       = "dpp"
	scoutSpanName   = "discovery.scout.html.dpp.discover"
)

var defaultConfig = htmlscout.Config{
	Name:     scoutName,
	Format:   scoutFormat,
	SpanName: scoutSpanName,
	Rules: []htmlscout.RuleConfig{
		{
			ItemSelector:        "a.news_abtn",
			LinkAttr:            "href",
			TitleSelector:       "h3",
			DateSelector:        "p.news_date",
			DateLayout:          scoutDateLayout,
			DescriptionSelector: "p:not(.news_date)",
		},
		{
			ItemSelector:  "div.bar.PL15",
			LinkSelector:  "a.news_list",
			LinkAttr:      "href",
			TitleSelector: "a.news_list",
			DateSelector:  "p.news_list_date",
			DateLayout:    scoutDateLayout,
		},
	},
}

type Scout = htmlscout.Scout

func New(logger *slog.Logger, tracer trace.Tracer, client *http.Client) (*Scout, error) {
	return htmlscout.New(logger, tracer, client, defaultConfig)
}
