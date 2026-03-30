package backfiller

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"text/template"

	"go.opentelemetry.io/otel/trace"
)

var (
	ErrEmptyPagerURLTemplate = fmt.Errorf("%w: url_template", ErrParamMissing)
)

var TemplateFuncMap = map[string]any{
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	"mul": func(a, b int) int { return a * b },
	"div": func(a, b int) int { return a / b },
}

type PagerMode string

const (
	PageModeIndex     PagerMode = "index"
	PageModeCursor    PagerMode = "cursor"
	PageModeDateRange PagerMode = "date-range"
)

type IndexPagerConfig struct {
	URLTemplate string            `json:"url_template,omitempty"`
	First       int               `json:"first,omitempty"`
	Step        int               `json:"step,omitempty"`
	Mode        PagerMode         `json:"mode,omitempty"`
	Params      map[string]string `json:"params,omitempty"`
	OmitFirst   bool              `json:"omit_first,omitempty"`
}

type PagerVars struct {
	URLTemplate string `json:"url_template,omitempty"`
	Value       int    `json:"value,omitempty"`
	First       int    `json:"first,omitempty"`
	Step        int    `json:"step,omitempty"`
}

type IndexPager struct {
	logger      *slog.Logger
	tracer      trace.Tracer
	cfg         IndexPagerConfig
	urlTmpl     *template.Template
	paramsTmpls map[string]*template.Template
	state       int
	first       bool
}

func NewIndexPager(logger *slog.Logger, tracer trace.Tracer, cfg IndexPagerConfig) (*IndexPager, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if tracer == nil {
		return nil, fmt.Errorf("%w: tracer", ErrParamMissing)
	}

	cfg.URLTemplate = strings.TrimSpace(cfg.URLTemplate)
	if cfg.URLTemplate == "" {
		return nil, ErrEmptyPagerURLTemplate
	}

	if cfg.Step <= 0 {
		cfg.Step = 1
	}

	fMap := template.FuncMap(TemplateFuncMap)

	urlTmpl, err := template.New("url").Funcs(fMap).Parse(cfg.URLTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse url template: %w", err)
	}

	paramsTmpls := make(map[string]*template.Template)
	for k, v := range cfg.Params {
		t, err := template.New(k).Funcs(fMap).Parse(v)
		if err != nil {
			return nil, fmt.Errorf("parse param template [%s]: %w", k, err)
		}
		paramsTmpls[k] = t
	}

	return &IndexPager{
		logger:      logger,
		tracer:      tracer,
		cfg:         cfg,
		urlTmpl:     urlTmpl,
		paramsTmpls: paramsTmpls,
		state:       cfg.First,
		first:       true,
	}, nil
}

func (p *IndexPager) Next(ctx context.Context) (string, error) {
	if p == nil {
		return "", nil
	}

	current := p.state
	isFirst := p.first

	if p.first {
		p.first = false
	} else {
		p.state += p.cfg.Step
		current = p.state
	}

	vars := PagerVars{
		URLTemplate: p.cfg.URLTemplate,
		Value:       current,
		First:       p.cfg.First,
		Step:        p.cfg.Step,
	}

	var buf bytes.Buffer
	if err := p.urlTmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("execute url template: %w", err)
	}

	u, err := url.Parse(buf.String())
	if err != nil {
		return "", fmt.Errorf("parse rendered url: %w", err)
	}

	if !isFirst || !p.cfg.OmitFirst {
		query := u.Query()
		for k, tmpl := range p.paramsTmpls {
			var pBuf bytes.Buffer
			if err := tmpl.Execute(&pBuf, vars); err != nil {
				return "", fmt.Errorf("execute param template [%s]: %w", k, err)
			}
			query.Set(k, pBuf.String())
		}
		u.RawQuery = query.Encode()
	}

	finalURL := u.String()

	p.logger.DebugContext(ctx, "index pager resolved next url",
		slog.String("url", finalURL),
		slog.Int("state", current),
		slog.String("mode", string(p.cfg.Mode)),
	)

	return finalURL, nil
}
