package transformer

import (
	"context"

	"github.com/ChiaYuChang/prism/internal/collector"
)

// NoOpTransformer is the Stage 2 placeholder. It passes minified content
// through unchanged until a real semantic transformation is needed.
type NoOpTransformer struct{}

var _ collector.Transformer = (*NoOpTransformer)(nil)

func NewNoOpTransformer() *NoOpTransformer {
	return &NoOpTransformer{}
}

func (t *NoOpTransformer) Transform(_ context.Context, minified string) (string, error) {
	return minified, nil
}
