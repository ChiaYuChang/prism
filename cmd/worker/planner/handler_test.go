package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/discovery"
	"github.com/ChiaYuChang/prism/internal/message"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestHandlerHandleMessagePlansMediaTasks(t *testing.T) {
	batchID := uuid.Must(uuid.NewV7())
	planner := &stubPlanner{
		plan: func(_ context.Context, req discovery.PlannerRequest) (discovery.PlannerResult, error) {
			require.Equal(t, batchID, req.BatchID)
			require.Equal(t, "trace-123", req.TraceID)
			require.Len(t, req.Targets, 1)
			require.Equal(t, "yahoo", req.Targets[0].SourceAbbr)
			require.Equal(t, "tw.news.yahoo.com", req.Targets[0].Site)
			return discovery.PlannerResult{TasksCreated: 4}, nil
		},
	}
	targets := []discovery.PlannerTarget{{SourceAbbr: "yahoo", URL: "https://tw.news.yahoo.com", Site: "tw.news.yahoo.com"}}
	h, err := NewHandler(testPlannerWorkerLogger(), noop.NewTracerProvider().Tracer("test"), planner, targets)
	require.NoError(t, err)

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
	_, err := NewHandler(testPlannerWorkerLogger(), noop.NewTracerProvider().Tracer("test"), planner, nil)
	require.ErrorIs(t, err, ErrNoPlannerTargets)
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
