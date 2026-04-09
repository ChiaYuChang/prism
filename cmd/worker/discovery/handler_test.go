package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	discoverymocks "github.com/ChiaYuChang/prism/internal/discovery/mocks"
	discoverysink "github.com/ChiaYuChang/prism/internal/discovery/sink"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/repo"
	repomocks "github.com/ChiaYuChang/prism/internal/repo/mocks"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestHandlerHandleMessageCompletesTask(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, sink, scoutRepo, scheduler)
	require.NoError(t, err)

	source := repo.Source{
		ID:      1,
		Type:    SourceTypeParty,
		BaseURL: "https://www.dpp.org.tw",
	}
	candidates := []model.Candidates{{Title: "A", URL: "https://www.dpp.org.tw/article/1"}}

	scoutRepo.EXPECT().GetSourceByID(mock.Anything, int32(1)).Return(source, nil)
	scout.EXPECT().Discover(mock.Anything, "https://www.dpp.org.tw/media/00").Return(candidates, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(nil)

	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       TaskKindDirectoryFetch,
		SourceType: SourceTypeParty,
		SourceID:   1,
		URL:        "https://www.dpp.org.tw/media/00",
		TraceID:    "trace-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.NoError(t, err)
	require.True(t, ack)
	require.NotNil(t, sink.last)
	require.Equal(t, int32(1), sink.last.SourceID)
	require.Equal(t, SourceTypeParty, sink.last.SourceType)
	require.Equal(t, "DIRECTORY", sink.last.IngestionMethod)
	require.Equal(t, batchID, sink.last.BatchID)
}

func TestHandlerHandleMessageFailsUnsupportedTask(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, sink, scoutRepo, scheduler)
	require.NoError(t, err)

	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    uuid.Must(uuid.NewV7()),
		Kind:       "PAGE_FETCH",
		SourceType: SourceTypeParty,
		SourceID:   1,
		URL:        "https://www.dpp.org.tw/media/00",
		TraceID:    "trace-123",
	}).Marshal()
	require.NoError(t, err)

	scheduler.EXPECT().FailTask(mock.Anything, taskID).Return(nil)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.Error(t, err)
	require.True(t, ack)
	require.ErrorIs(t, err, ErrUnsupportedTaskKind)
}

func TestHandlerHandleMessageNacksWhenCompleteFails(t *testing.T) {
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	scout := discoverymocks.NewMockScout(t)
	scoutRepo := repomocks.NewMockScout(t)
	scheduler := repomocks.NewMockScheduler(t)
	sink := &stubCandidateSink{}

	h, err := NewHandler(testLogger(), noop.NewTracerProvider().Tracer("test"), scout, sink, scoutRepo, scheduler)
	require.NoError(t, err)

	source := repo.Source{
		ID:      1,
		Type:    SourceTypeParty,
		BaseURL: "https://www.dpp.org.tw",
	}
	scoutRepo.EXPECT().GetSourceByID(mock.Anything, int32(1)).Return(source, nil)
	scout.EXPECT().Discover(mock.Anything, "https://www.dpp.org.tw/media/00").Return([]model.Candidates{}, nil)
	scheduler.EXPECT().CompleteTask(mock.Anything, taskID).Return(errors.New("db down"))

	payload, err := (&message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       TaskKindDirectoryFetch,
		SourceType: SourceTypeParty,
		SourceID:   1,
		URL:        "https://www.dpp.org.tw/media/00",
		TraceID:    "trace-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.Error(t, err)
	require.False(t, ack)
}

type stubCandidateSink struct {
	last *discoverysink.CandidateSinkRequest
	err  error
}

func (s *stubCandidateSink) Handle(_ context.Context, req discoverysink.CandidateSinkRequest) error {
	s.last = &req
	return s.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
