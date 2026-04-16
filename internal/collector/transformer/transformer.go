// Package transformer provides Stage 2 of the content transformation pipeline.
// Stage 1 (noise removal and size reduction) is handled by the minifier package.
// Stage 2 applies semantic transformations to minified content — currently a no-op
// for HTML but meaningful for future non-HTML inputs such as API responses.
package transformer

import (
	"context"

	"github.com/ChiaYuChang/prism/internal/collector"
)

// Chain composes multiple Transformers into a single Transformer.
// Each transformer is applied in order; an empty Chain behaves like NoOp.
type Chain []collector.Transformer

var _ collector.Transformer = (Chain)(nil)

func (c Chain) Transform(ctx context.Context, input string) (string, error) {
	var err error
	for _, t := range c {
		input, err = t.Transform(ctx, input)
		if err != nil {
			return "", err
		}
	}
	return input, nil
}
