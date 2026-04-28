package collector_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

// anyCtx matches any ctx argument; Dispatcher wraps the caller's ctx in a
// span, so asserting on a specific ctx value would be brittle.
var anyCtx = mock.Anything

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stageMocks holds per-test mocks so tests can set stage-specific
// expectations without rewriting the Pipeline plumbing every time.
type stageMocks struct {
	Fetcher     *mocks.MockFetcher
	Minifier    *mocks.MockTransformer
	Transformer *mocks.MockTransformer
	Parser      *mocks.MockParser
}

func newStageMocks(t *testing.T) (collector.Pipeline, stageMocks) {
	m := stageMocks{
		Fetcher:     mocks.NewMockFetcher(t),
		Minifier:    mocks.NewMockTransformer(t),
		Transformer: mocks.NewMockTransformer(t),
		Parser:      mocks.NewMockParser(t),
	}
	p := collector.Pipeline{
		Fetcher:      m.Fetcher,
		Minifier:     m.Minifier,
		Transformers: []collector.Transformer{m.Transformer},
		Parser:       m.Parser,
	}
	return p, m
}

func TestNewDispatcher_NilGuards(t *testing.T) {
	logger := silentLogger()
	tracer := noop.NewTracerProvider().Tracer("test")
	reg := collector.NewPipelineRegistry(collector.Pipeline{})

	tests := []struct {
		name     string
		build    func() (*collector.Dispatcher, error)
		wantWord string
	}{
		{
			name:     "nil logger",
			build:    func() (*collector.Dispatcher, error) { return collector.NewDispatcher(nil, tracer, reg) },
			wantWord: "logger",
		},
		{
			name:     "nil tracer",
			build:    func() (*collector.Dispatcher, error) { return collector.NewDispatcher(logger, nil, reg) },
			wantWord: "tracer",
		},
		{
			name:     "nil registry",
			build:    func() (*collector.Dispatcher, error) { return collector.NewDispatcher(logger, tracer, nil) },
			wantWord: "registry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := tt.build()
			assert.Nil(t, d)
			require.Error(t, err)
			assert.ErrorIs(t, err, collector.ErrParamMissing)
			assert.Contains(t, err.Error(), tt.wantWord)
		})
	}
}

func TestDispatcher_Dispatch_Success(t *testing.T) {
	const (
		sourceID  = "dpp"
		url       = "https://www.dpp.org.tw/media/contents/11540"
		raw       = "<html><body>raw</body></html>"
		minified  = "<html><body>min</body></html>"
		canonical = "<html><body>canonical</body></html>"
		title     = "press release title"
	)

	p, m := newStageMocks(t)

	m.Fetcher.EXPECT().Fetch(anyCtx, url).Return(raw, nil).Once()
	m.Minifier.EXPECT().Transform(anyCtx, raw).Return(minified, nil).Once()
	m.Transformer.EXPECT().Transform(anyCtx, minified).Return(canonical, nil).Once()
	m.Parser.EXPECT().Parse(anyCtx, url, canonical).
		Return(&collector.Article{URL: url, Title: title}, nil).Once()

	reg := collector.NewPipelineRegistry(p)
	d, err := collector.NewDispatcher(silentLogger(), noop.NewTracerProvider().Tracer("test"), reg)
	require.NoError(t, err)

	result, err := d.Dispatch(context.Background(), sourceID, url)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, title, result.Article.Title)
	assert.Equal(t, url, result.Article.URL)
	// Canonical (= last Transform output, input to Parser) is the documented
	// archive point for the success path.
	assert.Equal(t, canonical, result.Canonical)
}

// TestDispatcher_Dispatch_EmptyTransformers verifies that a Pipeline with no
// post-archive transformers still works: minified content is fed directly to
// the Parser, and Canonical equals the Minifier output.
func TestDispatcher_Dispatch_EmptyTransformers(t *testing.T) {
	const (
		url      = "https://example.test/x"
		raw      = "raw"
		minified = "minified-is-canonical"
	)
	fetcher := mocks.NewMockFetcher(t)
	minifier := mocks.NewMockTransformer(t)
	parser := mocks.NewMockParser(t)

	fetcher.EXPECT().Fetch(anyCtx, url).Return(raw, nil).Once()
	minifier.EXPECT().Transform(anyCtx, raw).Return(minified, nil).Once()
	parser.EXPECT().Parse(anyCtx, url, minified).
		Return(&collector.Article{URL: url}, nil).Once()

	p := collector.Pipeline{
		Fetcher:  fetcher,
		Minifier: minifier,
		Parser:   parser,
	}
	reg := collector.NewPipelineRegistry(p)
	d, err := collector.NewDispatcher(silentLogger(), noop.NewTracerProvider().Tracer("test"), reg)
	require.NoError(t, err)

	result, err := d.Dispatch(context.Background(), "", url)
	require.NoError(t, err)
	assert.Equal(t, minified, result.Canonical)
}

func TestDispatcher_Dispatch_StageErrors(t *testing.T) {
	const (
		url       = "https://x.example/y"
		raw       = "raw-html"
		minified  = "min-html"
		canonical = "canonical"
	)

	// Each case sets up mocks so stages up to (and including) the target
	// fail-stage are invoked; downstream stages must not be called.
	// Mockery's AssertExpectations (via NewMock*) fails the test if a
	// downstream stage is invoked without an expectation.
	tests := []struct {
		name             string
		setup            func(stageMocks) error
		wantStage        collector.PipelineStage
		wantIntermediate string
	}{
		{
			name: "fetch fails",
			setup: func(m stageMocks) error {
				boom := errors.New("network down")
				m.Fetcher.EXPECT().Fetch(anyCtx, url).Return("", boom).Once()
				return boom
			},
			wantStage:        collector.PipelineStageFetch,
			wantIntermediate: "",
		},
		{
			name: "minify fails",
			setup: func(m stageMocks) error {
				boom := errors.New("minify blew up")
				m.Fetcher.EXPECT().Fetch(anyCtx, url).Return(raw, nil).Once()
				m.Minifier.EXPECT().Transform(anyCtx, raw).Return("", boom).Once()
				return boom
			},
			wantStage:        collector.PipelineStageMinify,
			wantIntermediate: raw,
		},
		{
			name: "transform fails",
			setup: func(m stageMocks) error {
				boom := errors.New("transform boom")
				m.Fetcher.EXPECT().Fetch(anyCtx, url).Return(raw, nil).Once()
				m.Minifier.EXPECT().Transform(anyCtx, raw).Return(minified, nil).Once()
				m.Transformer.EXPECT().Transform(anyCtx, minified).Return("", boom).Once()
				return boom
			},
			wantStage:        collector.PipelineStageTransform,
			wantIntermediate: minified,
		},
		{
			name: "parse fails",
			setup: func(m stageMocks) error {
				boom := errors.New("parse blew up")
				m.Fetcher.EXPECT().Fetch(anyCtx, url).Return(raw, nil).Once()
				m.Minifier.EXPECT().Transform(anyCtx, raw).Return(minified, nil).Once()
				m.Transformer.EXPECT().Transform(anyCtx, minified).Return(canonical, nil).Once()
				m.Parser.EXPECT().Parse(anyCtx, url, canonical).Return(nil, boom).Once()
				return boom
			},
			wantStage:        collector.PipelineStageParse,
			wantIntermediate: canonical,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, m := newStageMocks(t)
			underlying := tc.setup(m)

			reg := collector.NewPipelineRegistry(p)
			d, err := collector.NewDispatcher(silentLogger(), noop.NewTracerProvider().Tracer("test"), reg)
			require.NoError(t, err)

			result, err := d.Dispatch(context.Background(), "", url)
			assert.Nil(t, result)
			require.Error(t, err)

			var se *collector.StageError
			require.True(t, errors.As(err, &se), "expected *StageError, got %T", err)
			assert.Equal(t, tc.wantStage, se.Stage)
			assert.Equal(t, tc.wantIntermediate, se.Intermediate)
			assert.ErrorIs(t, err, underlying)
			assert.ErrorIs(t, err, &collector.StageError{Stage: tc.wantStage})
		})
	}
}

// TestDispatcher_RoutesBySourceID pins the routing behaviour: Dispatch looks
// up the Pipeline by source ID, so a registered per-source Pipeline
// supersedes the fallback for that source.
func TestDispatcher_RoutesBySourceID(t *testing.T) {
	const url = "https://x.example/y"

	// Fallback pipeline — must NOT be invoked when source is registered.
	fbFetcher := mocks.NewMockFetcher(t)
	fbMinifier := mocks.NewMockTransformer(t)
	fbParser := mocks.NewMockParser(t)
	fallback := collector.Pipeline{Fetcher: fbFetcher, Minifier: fbMinifier, Parser: fbParser}

	// Specific pipeline — the one that should run.
	specFetcher := mocks.NewMockFetcher(t)
	specMinifier := mocks.NewMockTransformer(t)
	specParser := mocks.NewMockParser(t)
	specific := collector.Pipeline{Fetcher: specFetcher, Minifier: specMinifier, Parser: specParser}

	specFetcher.EXPECT().Fetch(anyCtx, url).Return("raw", nil).Once()
	specMinifier.EXPECT().Transform(anyCtx, "raw").Return("min", nil).Once()
	specParser.EXPECT().Parse(anyCtx, url, "min").
		Return(&collector.Article{URL: url, Title: "t"}, nil).Once()

	reg := collector.NewPipelineRegistry(fallback)
	reg.Register("dpp", specific)

	d, err := collector.NewDispatcher(silentLogger(), noop.NewTracerProvider().Tracer("test"), reg)
	require.NoError(t, err)

	_, err = d.Dispatch(context.Background(), "dpp", url)
	require.NoError(t, err)
}

// --- errors.go helpers ---

func TestPipelineStage_IsValid(t *testing.T) {
	valid := []collector.PipelineStage{
		collector.PipelineStageFetch,
		collector.PipelineStageMinify,
		collector.PipelineStageTransform,
		collector.PipelineStageParse,
	}
	for _, s := range valid {
		assert.True(t, s.IsValid(), "%q should be valid", s)
	}
	for _, s := range []collector.PipelineStage{"", "unknown", "FETCH"} {
		assert.False(t, s.IsValid(), "%q should be invalid", s)
	}
}

func TestParsePipelineStage(t *testing.T) {
	tests := []struct {
		in      string
		want    collector.PipelineStage
		wantErr bool
	}{
		{"fetch", collector.PipelineStageFetch, false},
		{"minify", collector.PipelineStageMinify, false},
		{"transform", collector.PipelineStageTransform, false},
		{"parse", collector.PipelineStageParse, false},
		{"", "", true},
		{"unknown", "", true},
		{"FETCH", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := collector.ParsePipelineStage(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.in)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestStageError_Error(t *testing.T) {
	se := &collector.StageError{
		Stage: collector.PipelineStageMinify,
		Err:   errors.New("kaboom"),
	}
	assert.Equal(t, "minify: kaboom", se.Error())
}

func TestStageError_UnwrapAndIs(t *testing.T) {
	sentinel := errors.New("root cause")
	se := &collector.StageError{
		Stage: collector.PipelineStageParse,
		Err:   sentinel,
	}

	assert.ErrorIs(t, se, sentinel)
	assert.ErrorIs(t, se, &collector.StageError{Stage: collector.PipelineStageParse})
	assert.NotErrorIs(t, se, &collector.StageError{Stage: collector.PipelineStageMinify})
	assert.NotErrorIs(t, se, errors.New("other"))
}

