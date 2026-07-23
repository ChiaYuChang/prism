package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/llm"
	llmmocks "github.com/ChiaYuChang/prism/internal/llm/mocks"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	repomocks "github.com/ChiaYuChang/prism/internal/repo/mocks"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestHandlerEmbedsCandidateTitleAndBrief(t *testing.T) {
	candidateID := uuid.Must(uuid.NewV7())
	taskID := uuid.Must(uuid.NewV7())
	embedder := llmmocks.NewMockEmbedder(t)
	scout := repomocks.NewMockScout(t)
	pipeline := repomocks.NewMockPipeline(t)
	embeddings := repomocks.NewMockEmbeddings(t)
	reporter := repomocks.NewMockTaskReporter(t)

	description := "A short brief"
	scout.EXPECT().GetCandidateByID(mock.Anything, candidateID).Return(repo.Candidate{
		ID: candidateID, Title: "A title", Description: &description,
	}, nil)
	embeddings.EXPECT().GetCandidateEmbeddingInputHash(mock.Anything, candidateID, int16(7), repo.EmbeddingCategoryTitle).Return("", pgx.ErrNoRows)
	embeddings.EXPECT().GetCandidateEmbeddingInputHash(mock.Anything, candidateID, int16(7), repo.EmbeddingCategoryBrief).Return("", pgx.ErrNoRows)
	embeddings.EXPECT().UpsertCandidateEmbedding(mock.Anything, mock.MatchedBy(func(arg repo.CreateCandidateEmbeddingParams) bool {
		return arg.CandidateID == candidateID && arg.ModelID == 7 && arg.TraceID == "trace-1" && len(arg.Vector) == 3
	})).Return(repo.CandidateEmbedding{}, nil).Twice()
	embedder.EXPECT().Embed(mock.Anything, mock.MatchedBy(func(req *llm.EmbedRequest) bool {
		return req.Model == "embed-model" && req.Dimensions == 3 && len(req.Input) == 2 && req.Input[0] == "A title" && req.Input[1] == description
	})).Return(&llm.EmbedResponse{Model: "embed-model", Vectors: [][]float32{{1, 2, 3}, {4, 5, 6}}}, nil)
	reporter.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	h, err := NewHandler(slog.Default(), noop.NewTracerProvider().Tracer("test"), embedder, scout, pipeline, embeddings, reporter, nil, 7, "embed-model", 3, 3)
	require.NoError(t, err)
	msg := taskMessage(t, message.TaskSignal{TaskID: taskID, Kind: repo.TaskKindEmbedCandidate, Meta: mustJSON(t, map[string]string{"candidate_id": candidateID.String()}), TraceID: "trace-1"})
	ack, err := h.HandleMessage(context.Background(), msg)
	require.True(t, ack)
	require.NoError(t, err)
}

func TestHandlerEmbedsCanonicalContent(t *testing.T) {
	contentID := uuid.Must(uuid.NewV7())
	taskID := uuid.Must(uuid.NewV7())
	author := "Author"
	embedder := llmmocks.NewMockEmbedder(t)
	scout := repomocks.NewMockScout(t)
	pipeline := repomocks.NewMockPipeline(t)
	embeddings := repomocks.NewMockEmbeddings(t)
	reporter := repomocks.NewMockTaskReporter(t)
	published := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	content := repo.Content{ID: contentID, Type: repo.ContentTypePartyRelease, SourceAbbr: "dpp", Title: "Release", Content: "Body", Author: &author, PublishedAt: published}
	pipeline.EXPECT().GetContentByID(mock.Anything, contentID).Return(content, nil)
	embeddings.EXPECT().GetContentEmbeddingInputHash(mock.Anything, contentID, int16(7)).Return("", pgx.ErrNoRows)
	embeddings.EXPECT().UpsertContentEmbedding(mock.Anything, mock.MatchedBy(func(arg repo.CreateContentEmbeddingParams) bool {
		return arg.ContentID == contentID && arg.ModelID == 7 && len(arg.Vector) == 3 && len(arg.InputHash) == 64
	})).Return(repo.ContentEmbedding{}, nil)
	embedder.EXPECT().Embed(mock.Anything, mock.MatchedBy(func(req *llm.EmbedRequest) bool {
		return req.Dimensions == 3 && len(req.Input) == 1 && req.Input[0] == canonicalDocumentText(content)
	})).Return(&llm.EmbedResponse{Vectors: [][]float32{{1, 2, 3}}}, nil)
	reporter.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	h, err := NewHandler(slog.Default(), noop.NewTracerProvider().Tracer("test"), embedder, scout, pipeline, embeddings, reporter, nil, 7, "embed-model", 3, 3)
	require.NoError(t, err)
	msg := taskMessage(t, message.TaskSignal{TaskID: taskID, Kind: repo.TaskKindEmbedContent, Meta: mustJSON(t, map[string]string{"content_id": contentID.String()}), TraceID: "trace-2"})
	ack, err := h.HandleMessage(context.Background(), msg)
	require.True(t, ack)
	require.NoError(t, err)
}

func TestHandlerRejectsWrongVectorDimension(t *testing.T) {
	candidateID := uuid.Must(uuid.NewV7())
	taskID := uuid.Must(uuid.NewV7())
	embedder := llmmocks.NewMockEmbedder(t)
	scout := repomocks.NewMockScout(t)
	pipeline := repomocks.NewMockPipeline(t)
	embeddings := repomocks.NewMockEmbeddings(t)
	reporter := repomocks.NewMockTaskReporter(t)
	scout.EXPECT().GetCandidateByID(mock.Anything, candidateID).Return(repo.Candidate{ID: candidateID, Title: "Title"}, nil)
	embeddings.EXPECT().GetCandidateEmbeddingInputHash(mock.Anything, candidateID, int16(7), repo.EmbeddingCategoryTitle).Return("", pgx.ErrNoRows)
	embedder.EXPECT().Embed(mock.Anything, mock.Anything).Return(&llm.EmbedResponse{Vectors: [][]float32{{1, 2}}}, nil)
	reporter.EXPECT().FailTask(mock.Anything, taskID, 3, mock.MatchedBy(func(value string) bool { return value != "" })).Return(nil)

	h, err := NewHandler(slog.Default(), noop.NewTracerProvider().Tracer("test"), embedder, scout, pipeline, embeddings, reporter, nil, 7, "embed-model", 3, 3)
	require.NoError(t, err)
	msg := taskMessage(t, message.TaskSignal{TaskID: taskID, Kind: repo.TaskKindEmbedCandidate, Meta: mustJSON(t, map[string]string{"candidate_id": candidateID.String()})})
	ack, err := h.HandleMessage(context.Background(), msg)
	require.True(t, ack)
	require.ErrorIs(t, err, ErrInvalidEmbedding)
}

func taskMessage(t *testing.T, signal message.TaskSignal) *wm.Message {
	t.Helper()
	payload, err := signal.Marshal()
	require.NoError(t, err)
	return wm.NewMessage("test", payload)
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(value)
	require.NoError(t, err)
	return payload
}
