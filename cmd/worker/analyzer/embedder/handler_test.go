package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/analyzer/embedder"
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
	llmEmbedder := llmmocks.NewMockEmbedder(t)
	scout := repomocks.NewMockScout(t)
	pipeline := repomocks.NewMockPipeline(t)
	embeddings := repomocks.NewMockEmbeddings(t)
	scheduler := repomocks.NewMockScheduler(t)
	tasks := repomocks.NewMockTasks(t)

	tasks.EXPECT().IsTaskRunning(mock.Anything, taskID).Return(true, nil)

	description := "A short brief"
	scout.EXPECT().GetCandidateByID(mock.Anything, candidateID).Return(repo.Candidate{
		ID: candidateID, Title: "A title", Description: &description,
	}, nil)
	embeddings.EXPECT().GetCandidateEmbeddingInputHash(mock.Anything, candidateID, int16(7), repo.EmbeddingCategoryTitle).Return("", pgx.ErrNoRows)
	embeddings.EXPECT().GetCandidateEmbeddingInputHash(mock.Anything, candidateID, int16(7), repo.EmbeddingCategoryBrief).Return("", pgx.ErrNoRows)
	embeddings.EXPECT().UpsertCandidateEmbedding(mock.Anything, mock.MatchedBy(func(arg repo.CreateCandidateEmbeddingParams) bool {
		return arg.CandidateID == candidateID && arg.ModelID == 7 && arg.TraceID == "trace-1" && len(arg.Vector) == 3
	})).Return(repo.CandidateEmbedding{}, nil).Twice()
	llmEmbedder.EXPECT().Embed(mock.Anything, mock.MatchedBy(func(req *llm.EmbedRequest) bool {
		return req.Model == "embed-model" && req.Dimensions == 3 && len(req.Input) == 2 && req.Input[0] == "A title" && req.Input[1] == description
	})).Return(&llm.EmbedResponse{Model: "embed-model", Vectors: [][]float32{{1, 2, 3}, {4, 5, 6}}}, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	h, err := NewHandler(HandlerConfig{
		Logger: slog.Default(),
		Tracer: noop.NewTracerProvider().Tracer("test"),
		Embedder: EmbedderConfig{
			Embedder:  llmEmbedder,
			ModelID:   7,
			ModelName: "embed-model",
			Dimension: 3,
			RetryMax:  3,
		},
		Store: Store{
			Scout:      scout,
			Pipeline:   pipeline,
			Embeddings: embeddings,
			Reporter:   scheduler,
			Tasks:      tasks,
		},
	})
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
	llmEmbedder := llmmocks.NewMockEmbedder(t)
	scout := repomocks.NewMockScout(t)
	pipeline := repomocks.NewMockPipeline(t)
	embeddings := repomocks.NewMockEmbeddings(t)
	scheduler := repomocks.NewMockScheduler(t)
	tasks := repomocks.NewMockTasks(t)

	tasks.EXPECT().IsTaskRunning(mock.Anything, taskID).Return(true, nil)

	published := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	content := repo.Content{ID: contentID, Type: repo.ContentTypePartyRelease, SourceAbbr: "dpp", Title: "Release", Content: "Body", Author: &author, PublishedAt: published}
	pipeline.EXPECT().GetContentByID(mock.Anything, contentID).Return(content, nil)
	embeddings.EXPECT().GetContentEmbeddingInputHash(mock.Anything, contentID, int16(7)).Return("", pgx.ErrNoRows)
	embeddings.EXPECT().UpsertContentEmbedding(mock.Anything, mock.MatchedBy(func(arg repo.CreateContentEmbeddingParams) bool {
		return arg.ContentID == contentID && arg.ModelID == 7 && len(arg.Vector) == 3 && len(arg.InputHash) == 64
	})).Return(repo.ContentEmbedding{}, nil)
	llmEmbedder.EXPECT().Embed(mock.Anything, mock.MatchedBy(func(req *llm.EmbedRequest) bool {
		return req.Dimensions == 3 && len(req.Input) == 1 && req.Input[0] == embedder.CanonicalDocumentText(content)
	})).Return(&llm.EmbedResponse{Vectors: [][]float32{{1, 2, 3}}}, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	h, err := NewHandler(HandlerConfig{
		Logger: slog.Default(),
		Tracer: noop.NewTracerProvider().Tracer("test"),
		Embedder: EmbedderConfig{
			Embedder:  llmEmbedder,
			ModelID:   7,
			ModelName: "embed-model",
			Dimension: 3,
			RetryMax:  3,
		},
		Store: Store{
			Scout:      scout,
			Pipeline:   pipeline,
			Embeddings: embeddings,
			Reporter:   scheduler,
			Tasks:      tasks,
		},
	})
	require.NoError(t, err)
	msg := taskMessage(t, message.TaskSignal{TaskID: taskID, Kind: repo.TaskKindEmbedContent, Meta: mustJSON(t, map[string]string{"content_id": contentID.String()}), TraceID: "trace-2"})
	ack, err := h.HandleMessage(context.Background(), msg)
	require.True(t, ack)
	require.NoError(t, err)
}

func TestHandlerEmbedsContentSnapshotAfterSoftDelete(t *testing.T) {
	contentID := uuid.Must(uuid.NewV7())
	taskID := uuid.Must(uuid.NewV7())
	llmEmbedder := llmmocks.NewMockEmbedder(t)
	scout := repomocks.NewMockScout(t)
	pipeline := repomocks.NewMockPipeline(t)
	embeddings := repomocks.NewMockEmbeddings(t)
	scheduler := repomocks.NewMockScheduler(t)
	tasks := repomocks.NewMockTasks(t)
	deletedAt := time.Now()
	snapshot := repo.Content{
		ID: contentID, Type: repo.ContentTypePartyRelease, SourceAbbr: "dpp", Title: "Captured release",
		Content: "Captured body", PublishedAt: time.Now(), DeletedAt: &deletedAt, Metadata: []byte(`{"captured":true}`),
	}

	tasks.EXPECT().IsTaskRunning(mock.Anything, taskID).Return(true, nil)
	embeddings.EXPECT().GetContentEmbeddingInputHash(mock.Anything, contentID, int16(7)).Return("", pgx.ErrNoRows)
	llmEmbedder.EXPECT().Embed(mock.Anything, mock.MatchedBy(func(req *llm.EmbedRequest) bool {
		return len(req.Input) == 1 && req.Input[0] == embedder.CanonicalDocumentText(snapshot)
	})).Return(&llm.EmbedResponse{Vectors: [][]float32{{1, 2, 3}}}, nil)
	embeddings.EXPECT().UpsertContentEmbedding(mock.Anything, mock.MatchedBy(func(arg repo.CreateContentEmbeddingParams) bool {
		return arg.ContentID == contentID && arg.TraceID == "snapshot-trace"
	})).Return(repo.ContentEmbedding{}, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	h, err := NewHandler(HandlerConfig{
		Logger: slog.Default(), Tracer: noop.NewTracerProvider().Tracer("test"),
		Embedder: EmbedderConfig{Embedder: llmEmbedder, ModelID: 7, ModelName: "embed-model", Dimension: 3, RetryMax: 3},
		Store:    Store{Scout: scout, Pipeline: pipeline, Embeddings: embeddings, Reporter: scheduler, Tasks: tasks},
	})
	require.NoError(t, err)
	msg := taskMessage(t, message.TaskSignal{TaskID: taskID, Kind: repo.TaskKindEmbedContent, TraceID: "snapshot-trace", Meta: mustJSON(t, map[string]any{
		"content_id": contentID.String(), "snapshot": snapshot,
	})})
	ack, err := h.HandleMessage(context.Background(), msg)
	require.True(t, ack)
	require.NoError(t, err)
}

func TestHandlerRejectsWrongVectorDimension(t *testing.T) {
	candidateID := uuid.Must(uuid.NewV7())
	taskID := uuid.Must(uuid.NewV7())
	llmEmbedder := llmmocks.NewMockEmbedder(t)
	scout := repomocks.NewMockScout(t)
	pipeline := repomocks.NewMockPipeline(t)
	embeddings := repomocks.NewMockEmbeddings(t)
	scheduler := repomocks.NewMockScheduler(t)
	tasks := repomocks.NewMockTasks(t)

	tasks.EXPECT().IsTaskRunning(mock.Anything, taskID).Return(true, nil)

	scout.EXPECT().GetCandidateByID(mock.Anything, candidateID).Return(repo.Candidate{ID: candidateID, Title: "Title"}, nil)
	embeddings.EXPECT().GetCandidateEmbeddingInputHash(mock.Anything, candidateID, int16(7), repo.EmbeddingCategoryTitle).Return("", pgx.ErrNoRows)
	llmEmbedder.EXPECT().Embed(mock.Anything, mock.Anything).Return(&llm.EmbedResponse{Vectors: [][]float32{{1, 2}}}, nil)
	scheduler.EXPECT().FailTask(mock.Anything, taskID, 3, mock.MatchedBy(func(value string) bool { return value != "" })).Return(nil)

	h, err := NewHandler(HandlerConfig{
		Logger: slog.Default(),
		Tracer: noop.NewTracerProvider().Tracer("test"),
		Embedder: EmbedderConfig{
			Embedder:  llmEmbedder,
			ModelID:   7,
			ModelName: "embed-model",
			Dimension: 3,
			RetryMax:  3,
		},
		Store: Store{
			Scout:      scout,
			Pipeline:   pipeline,
			Embeddings: embeddings,
			Reporter:   scheduler,
			Tasks:      tasks,
		},
	})
	require.NoError(t, err)
	msg := taskMessage(t, message.TaskSignal{TaskID: taskID, Kind: repo.TaskKindEmbedCandidate, Meta: mustJSON(t, map[string]string{"candidate_id": candidateID.String()})})
	ack, err := h.HandleMessage(context.Background(), msg)
	require.True(t, ack)
	require.ErrorIs(t, err, embedder.ErrInvalidEmbedding)
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
