package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	collector "github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/internal/collector/mocks"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

var errContentNotFound = errors.New("content not found")

func TestHandlerProcess_ArchivesStageErrorWithRecoverablePayloadKind(t *testing.T) {
	const (
		url       = "https://example.test/article"
		raw       = "raw-html"
		minified  = "minified-html"
		canonical = "canonical-html"
	)

	tests := []struct {
		name        string
		setup       func(t *testing.T) collector.Pipeline
		wantPayload string
		wantKind    archiver.PayloadKind
		wantStage   collector.PipelineStage
	}{
		{
			name: "minify failure archives raw payload",
			setup: func(t *testing.T) collector.Pipeline {
				fetcher := mocks.NewMockFetcher(t)
				minifier := mocks.NewMockTransformer(t)
				parser := mocks.NewMockParser(t)

				fetcher.EXPECT().Fetch(mock.Anything, url).Return(raw, nil).Once()
				minifier.EXPECT().Transform(mock.Anything, raw).Return("", errors.New("minify failed")).Once()

				return collector.Pipeline{Fetcher: fetcher, Minifier: minifier, Parser: parser}
			},
			wantPayload: raw,
			wantKind:    archiver.PayloadKindRaw,
			wantStage:   collector.PipelineStageMinify,
		},
		{
			name: "transform failure archives minified payload",
			setup: func(t *testing.T) collector.Pipeline {
				fetcher := mocks.NewMockFetcher(t)
				minifier := mocks.NewMockTransformer(t)
				transformer := mocks.NewMockTransformer(t)
				parser := mocks.NewMockParser(t)

				fetcher.EXPECT().Fetch(mock.Anything, url).Return(raw, nil).Once()
				minifier.EXPECT().Transform(mock.Anything, raw).Return(minified, nil).Once()
				transformer.EXPECT().Transform(mock.Anything, minified).Return("", errors.New("transform failed")).Once()

				return collector.Pipeline{Fetcher: fetcher, Minifier: minifier, Transformers: []collector.Transformer{transformer}, Parser: parser}
			},
			wantPayload: minified,
			wantKind:    archiver.PayloadKindMinified,
			wantStage:   collector.PipelineStageTransform,
		},
		{
			name: "parse failure archives canonical payload",
			setup: func(t *testing.T) collector.Pipeline {
				fetcher := mocks.NewMockFetcher(t)
				minifier := mocks.NewMockTransformer(t)
				transformer := mocks.NewMockTransformer(t)
				parser := mocks.NewMockParser(t)

				fetcher.EXPECT().Fetch(mock.Anything, url).Return(raw, nil).Once()
				minifier.EXPECT().Transform(mock.Anything, raw).Return(minified, nil).Once()
				transformer.EXPECT().Transform(mock.Anything, minified).Return(canonical, nil).Once()
				parser.EXPECT().Parse(mock.Anything, url, canonical).Return(nil, errors.New("parse failed")).Once()

				return collector.Pipeline{Fetcher: fetcher, Minifier: minifier, Transformers: []collector.Transformer{transformer}, Parser: parser}
			},
			wantPayload: canonical,
			wantKind:    archiver.PayloadKindCanonical,
			wantStage:   collector.PipelineStageParse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saver := &capturingSaver{}
			h := newTestHandler(t, tt.setup(t), saver)
			batchID := uuid.New()
			sig := message.TaskSignal{
				TaskID:     uuid.New(),
				BatchID:    batchID,
				TraceID:    "trace-123",
				Kind:       repo.TaskKindPageFetch,
				SourceType: repo.SourceTypeParty,
				SourceAbbr: "dpp",
				URL:        url,
			}

			err := h.process(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), sig)
			require.Error(t, err)
			require.Len(t, saver.records, 1)

			record := saver.records[0]
			assert.Equal(t, url, record.URL)
			assert.Equal(t, tt.wantPayload, record.Payload)
			assert.Equal(t, sig.TraceID, record.TraceID)
			assert.Equal(t, string(tt.wantKind), record.Metadata["kind"])
			assert.Equal(t, tt.wantStage, record.Metadata["recover_from"])
			assert.Equal(t, sig.TraceID, record.Metadata["recover_key"])
			assert.Equal(t, sig.SourceAbbr, record.Metadata["source_abbr"])
			assert.Equal(t, sig.SourceType, record.Metadata["source_type"])
			assert.Equal(t, batchID.String(), record.Metadata["batch_id"])
			assert.NotEmpty(t, record.Metadata["error"])
		})
	}
}

func newTestHandler(t *testing.T, p collector.Pipeline, saver collector.Saver) *Handler {
	t.Helper()

	dispatcher, err := collector.NewDispatcher(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		noop.NewTracerProvider().Tracer("test"),
		collector.NewPipelineRegistry(p),
	)
	require.NoError(t, err)

	h, err := NewHandler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		noop.NewTracerProvider().Tracer("test"),
		dispatcher,
		saver,
		nil,
		stubPipeline{},
		stubReporter{},
	)
	require.NoError(t, err)
	return h
}

type capturingSaver struct {
	records []collector.Archive
}

func (s *capturingSaver) Save(_ context.Context, record collector.Archive) error {
	s.records = append(s.records, record)
	return nil
}

type stubPipeline struct{}

func (stubPipeline) GetContentByID(context.Context, uuid.UUID) (repo.Content, error) {
	return repo.Content{}, errContentNotFound
}

func (stubPipeline) GetContentByURL(context.Context, string) (repo.Content, error) {
	return repo.Content{}, errContentNotFound
}

func (stubPipeline) GetContentByCandidateID(context.Context, uuid.UUID) (repo.Content, error) {
	return repo.Content{}, errContentNotFound
}

func (stubPipeline) CreateContent(context.Context, repo.CreateContentParams) (repo.Content, error) {
	return repo.Content{}, nil
}

func (stubPipeline) UpdateContentMetadata(context.Context, repo.UpdateContentMetadataParams) (repo.Content, error) {
	return repo.Content{}, nil
}

func (stubPipeline) ListContentsByBatchID(context.Context, uuid.UUID) ([]repo.Content, error) {
	return nil, nil
}

func (stubPipeline) ListRecentSeedContents(context.Context, int32) ([]repo.Content, error) {
	return nil, nil
}

type stubReporter struct{}

func (stubReporter) CompleteTask(context.Context, uuid.UUID) error { return nil }

func (stubReporter) FailTask(context.Context, uuid.UUID) error { return nil }
