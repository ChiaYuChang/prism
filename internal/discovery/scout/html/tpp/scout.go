package tpp

import (
	"log/slog"
	"net/http"

	htmlscout "github.com/ChiaYuChang/prism/internal/discovery/scout/html"
	"go.opentelemetry.io/otel/trace"
)

const (
	scoutDateLayout = "2006年01月02日"
	scoutFormat     = "html"
	scoutName       = "tpp"
	scoutSpanName   = "discovery.scout.html.tpp.discover"
)

var defaultConfig = htmlscout.Config{
	Name:     scoutName,
	Format:   scoutFormat,
	SpanName: scoutSpanName,
	Rules: []htmlscout.RuleConfig{
		{
			ItemSelector:        "div.latest_outer div.list_frame",
			LinkSelector:        "a",
			LinkAttr:            "href",
			TitleSelector:       "div.list_name",
			DateSelector:        "div.list_date",
			DateLayout:          scoutDateLayout,
			DescriptionSelector: "span.inner_tags",
		},
	},
}

type Scout = htmlscout.Scout

func New(logger *slog.Logger, tracer trace.Tracer, client *http.Client) (*Scout, error) {
	return htmlscout.New(logger, tracer, client, defaultConfig)
}
