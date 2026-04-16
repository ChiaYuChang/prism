package parser

import (
	"context"
	"fmt"

	"github.com/ChiaYuChang/prism/internal/collector"
)

type CompositeParser struct {
	html   collector.Parser
	jsonld collector.Parser
}

var _ collector.Parser = (*CompositeParser)(nil)

func NewCompositeParser(html, jsonld collector.Parser) (*CompositeParser, error) {
	if html == nil {
		return nil, fmt.Errorf("%w: html parser", ErrParamMissing)
	}
	if jsonld == nil {
		return nil, fmt.Errorf("%w: jsonld parser", ErrParamMissing)
	}
	return &CompositeParser{
		html:   html,
		jsonld: jsonld,
	}, nil
}

func (p *CompositeParser) Parse(ctx context.Context, url string, data string) (*collector.Article, error) {
	hResult, err := p.html.Parse(ctx, url, data)
	if err != nil {
		return nil, err
	}

	jResult, err := p.jsonld.Parse(ctx, url, data)
	if err != nil {
		return hResult, nil
	}

	merged := MergeArticleContent(hResult, jResult)
	merged.URL = url
	return merged, nil
}
