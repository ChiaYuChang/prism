package embedder_test

import (
	"context"

	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/analyzer/embedder"
	"github.com/ChiaYuChang/prism/internal/llm"
	llmmocks "github.com/ChiaYuChang/prism/internal/llm/mocks"
	"github.com/ChiaYuChang/prism/internal/repo"
	repomocks "github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestNewServiceValidation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")
	llmEmbedder := llmmocks.NewMockEmbedder(t)
	scout := repomocks.NewMockScout(t)
	pipeline := repomocks.NewMockPipeline(t)
	embeddings := repomocks.NewMockEmbeddings(t)

	_, err := embedder.NewService(nil, tracer, llmEmbedder, scout, pipeline, embeddings, 1, "model", 1536)
	require.ErrorIs(t, err, embedder.ErrParamMissing)

	_, err = embedder.NewService(logger, nil, llmEmbedder, scout, pipeline, embeddings, 1, "model", 1536)
	require.ErrorIs(t, err, embedder.ErrParamMissing)

	_, err = embedder.NewService(logger, tracer, nil, scout, pipeline, embeddings, 1, "model", 1536)
	require.ErrorIs(t, err, embedder.ErrParamMissing)

	svc, err := embedder.NewService(logger, tracer, llmEmbedder, scout, pipeline, embeddings, 1, "model", 1536)
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestEmbedArticle(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")
	llmEmbedder := llmmocks.NewMockEmbedder(t)
	scout := repomocks.NewMockScout(t)
	pipeline := repomocks.NewMockPipeline(t)
	embeddings := repomocks.NewMockEmbeddings(t)

	svc, err := embedder.NewService(logger, tracer, llmEmbedder, scout, pipeline, embeddings, 1, "text-embedding-3-small", 3)
	require.NoError(t, err)

	t.Run("empty text error", func(t *testing.T) {
		_, err := svc.EmbedArticle(context.Background(), "", "")
		require.ErrorIs(t, err, embedder.ErrNoEmbeddableText)
	})

	t.Run("successful embedding", func(t *testing.T) {
		llmEmbedder.EXPECT().Embed(mock.Anything, mock.MatchedBy(func(req *llm.EmbedRequest) bool {
			return req.Model == "text-embedding-3-small" && req.Dimensions == 3 && len(req.Input) == 1
		})).Return(&llm.EmbedResponse{
			Vectors: [][]float32{{0.1, 0.2, 0.3}},
		}, nil).Once()

		vec, err := svc.EmbedArticle(context.Background(), "Title", "Content")
		require.NoError(t, err)
		require.Equal(t, []float32{0.1, 0.2, 0.3}, vec)
	})
}

func TestEmbedContent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer("test")
	llmEmbedder := llmmocks.NewMockEmbedder(t)
	scout := repomocks.NewMockScout(t)
	pipeline := repomocks.NewMockPipeline(t)
	embeddings := repomocks.NewMockEmbeddings(t)

	svc, err := embedder.NewService(logger, tracer, llmEmbedder, scout, pipeline, embeddings, 1, "text-embedding-3-small", 3)
	require.NoError(t, err)

	contentID := uuid.New()
	content := repo.Content{
		ID:          contentID,
		Title:       "Test Headline",
		Type:        "PARTY",
		SourceAbbr:  "DPP",
		Content:     "Full news article content.",
		PublishedAt: time.Now(),
	}

	t.Run("embed content successfully", func(t *testing.T) {
		pipeline.EXPECT().GetContentByID(mock.Anything, contentID).Return(content, nil).Once()
		embeddings.EXPECT().GetContentEmbeddingInputHash(mock.Anything, contentID, int16(1)).Return("", pgx.ErrNoRows).Once()
		llmEmbedder.EXPECT().Embed(mock.Anything, mock.Anything).Return(&llm.EmbedResponse{
			Vectors: [][]float32{{0.5, 0.6, 0.7}},
		}, nil).Once()
		embeddings.EXPECT().UpsertContentEmbedding(mock.Anything, mock.MatchedBy(func(p repo.CreateContentEmbeddingParams) bool {
			return p.ContentID == contentID && p.ModelID == 1 && len(p.Vector) == 3
		})).Return(repo.ContentEmbedding{}, nil).Once()

		err := svc.EmbedContent(context.Background(), contentID, "trace-123")
		require.NoError(t, err)
	})
}
