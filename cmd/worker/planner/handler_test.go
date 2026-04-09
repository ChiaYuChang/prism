package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	repomocks "github.com/ChiaYuChang/prism/internal/repo/mocks"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestHandlerHandleMessagePlansMediaTasks(t *testing.T) {
	batchID := uuid.Must(uuid.NewV7())
	planner := &stubPlanner{
		plan: func(_ context.Context, req discovery.PlannerRequest) (discovery.PlannerResult, error) {
			require.Equal(t, batchID, req.BatchID)
			require.Equal(t, "trace-123", req.TraceID)
			require.Len(t, req.Targets, 2)
			require.Equal(t, int32(10), req.Targets[0].SourceID)
			require.Equal(t, "news.example.com", req.Targets[0].Site)
			return discovery.PlannerResult{TasksCreated: 4}, nil
		},
	}
	scout := repomocks.NewMockScout(t)
	h, err := NewHandler(testPlannerWorkerLogger(), noop.NewTracerProvider().Tracer("test"), planner, scout)
	require.NoError(t, err)
	scout.EXPECT().ListSourcesByType(mock.Anything, "MEDIA").Return([]repo.Source{
		{ID: 10, BaseURL: "https://news.example.com"},
		{ID: 20, BaseURL: "https://www.yahoo.com"},
	}, nil)

	payload, err := (&message.BatchCompletedSignal{
		BatchID:    batchID,
		SourceType: "PARTY",
		TraceID:    "trace-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.NoError(t, err)
	require.True(t, ack)
}

func TestHandlerHandleMessageReturnsNoTargets(t *testing.T) {
	planner := &stubPlanner{}
	scout := repomocks.NewMockScout(t)
	h, err := NewHandler(testPlannerWorkerLogger(), noop.NewTracerProvider().Tracer("test"), planner, scout)
	require.NoError(t, err)

	batchID := uuid.Must(uuid.NewV7())
	scout.EXPECT().ListSourcesByType(mock.Anything, "MEDIA").Return(nil, nil)

	payload, err := (&message.BatchCompletedSignal{
		BatchID:    batchID,
		SourceType: "PARTY",
		TraceID:    "trace-123",
	}).Marshal()
	require.NoError(t, err)

	ack, err := h.HandleMessage(context.Background(), wm.NewMessage("id", payload))
	require.ErrorIs(t, err, ErrNoPlannerTargets)
	require.True(t, ack)
}

func testPlannerWorkerLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type stubPlanner struct {
	plan func(context.Context, discovery.PlannerRequest) (discovery.PlannerResult, error)
}

func (s *stubPlanner) Plan(ctx context.Context, req discovery.PlannerRequest) (discovery.PlannerResult, error) {
	if s.plan != nil {
		return s.plan(ctx, req)
	}
	return discovery.PlannerResult{}, nil
}
