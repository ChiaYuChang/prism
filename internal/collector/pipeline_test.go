package collector_test

import (
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/mocks"
	"github.com/stretchr/testify/assert"
)

// buildPipeline returns a Pipeline whose fields are uniquely-identifiable
// mocks, so registry lookup comparisons can be done by pointer equality.
// We don't exercise the pipeline; we only check routing.
func buildPipeline(t *testing.T) collector.Pipeline {
	return collector.Pipeline{
		Fetcher:  mocks.NewMockFetcher(t),
		Minifier: mocks.NewMockTransformer(t),
		Parser:   mocks.NewMockParser(t),
	}
}

func TestPipelineRegistry_FallbackWhenEmpty(t *testing.T) {
	fallback := buildPipeline(t)
	reg := collector.NewPipelineRegistry(fallback)

	assert.Same(t, fallback.Fetcher, reg.For("dpp").Fetcher)
	assert.Same(t, fallback.Fetcher, reg.For("").Fetcher)
}

func TestPipelineRegistry_RegisteredWins(t *testing.T) {
	fallback := buildPipeline(t)
	specific := buildPipeline(t)
	reg := collector.NewPipelineRegistry(fallback)
	reg.Register("dpp", specific)

	assert.Same(t, specific.Fetcher, reg.For("dpp").Fetcher)
	assert.Same(t, fallback.Fetcher, reg.For("other").Fetcher)
}

func TestPipelineRegistry_ReRegisterOverwrites(t *testing.T) {
	fallback := buildPipeline(t)
	first := buildPipeline(t)
	second := buildPipeline(t)
	reg := collector.NewPipelineRegistry(fallback)

	reg.Register("dpp", first)
	reg.Register("dpp", second)

	assert.Same(t, second.Fetcher, reg.For("dpp").Fetcher)
}
