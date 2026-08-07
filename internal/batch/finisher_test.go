package batch

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

type finishedPublisher struct {
	signals []*message.PipelineBatchFinishedSignal
}

func (p *finishedPublisher) PublishPipelineBatchFinished(_ context.Context, signal *message.PipelineBatchFinishedSignal) error {
	p.signals = append(p.signals, signal)
	return nil
}

func TestFinisherMarksAndPublishesOnlyWinningTransitions(t *testing.T) {
	runtime := mocks.NewMockPipelineRuntime(t)
	batchID := uuid.New()
	rootID := uuid.New()
	ownerID := uuid.New()
	traceID := "trace"
	runtime.EXPECT().FindFinishedBatches(mock.Anything, int32(10)).Return([]repo.Batch{{
		ID: batchID, ParentID: &rootID, ParentTaskID: &ownerID, TraceID: &traceID,
		Succeeded: boolPtr(true), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}, nil)
	runtime.EXPECT().MarkBatchFinished(mock.Anything, batchID, true, traceID).Return(int64(1), nil)
	runtime.EXPECT().ListReadyPipelineBatches(mock.Anything, int32(10)).Return([]repo.Batch{{
		ID: batchID, ParentID: &rootID, ParentTaskID: &ownerID, TraceID: &traceID,
		Succeeded: boolPtr(true), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}, nil)
	runtime.EXPECT().MarkPipelinePublished(mock.Anything, batchID).Return(nil)
	runtime.EXPECT().FindFinishedRootBatches(mock.Anything, int32(10)).Return(nil, nil)
	publisher := &finishedPublisher{}
	finisher, err := NewFinisher(slog.Default(), noop.NewTracerProvider().Tracer("test"), runtime, publisher)
	require.NoError(t, err)

	count, err := finisher.Detect(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Len(t, publisher.signals, 1)
	require.Equal(t, rootID, publisher.signals[0].RootBatchID)
	require.Equal(t, ownerID, publisher.signals[0].OwnerTaskID)
}

func TestFinisherRetriesCommittedPipelineOutboxWithoutNewTransition(t *testing.T) {
	runtime := mocks.NewMockPipelineRuntime(t)
	batchID, rootID, ownerID := uuid.New(), uuid.New(), uuid.New()
	traceID := "trace"
	runtime.EXPECT().FindFinishedBatches(mock.Anything, int32(10)).Return(nil, nil)
	runtime.EXPECT().ListReadyPipelineBatches(mock.Anything, int32(10)).Return([]repo.Batch{{
		ID: batchID, ParentID: &rootID, ParentTaskID: &ownerID, TraceID: &traceID,
		Succeeded: boolPtr(true), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}, nil)
	runtime.EXPECT().MarkPipelinePublished(mock.Anything, batchID).Return(nil)
	runtime.EXPECT().FindFinishedRootBatches(mock.Anything, int32(10)).Return(nil, nil)
	publisher := &finishedPublisher{}
	finisher, err := NewFinisher(slog.Default(), noop.NewTracerProvider().Tracer("test"), runtime, publisher)
	require.NoError(t, err)

	count, err := finisher.Detect(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Len(t, publisher.signals, 1)
}

func boolPtr(value bool) *bool { return &value }
