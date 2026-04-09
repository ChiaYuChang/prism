package batch

import (
	"context"
	"testing"

	"github.com/ChiaYuChang/prism/internal/repo"
	repomocks "github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestScanCompletedBatchesReturnsOnlyCompletedPartyBatches(t *testing.T) {
	batchTrigger := repomocks.NewMockBatchTrigger(t)
	tr, err := New(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), batchTrigger)
	require.NoError(t, err)

	batchDone := uuid.Must(uuid.NewV7())
	batchPending := uuid.Must(uuid.NewV7())
	taskID := uuid.Must(uuid.NewV7())

	batchTrigger.EXPECT().ListRecentSeedContents(mock.Anything, int32(10)).Return([]repo.Content{
		{BatchID: batchDone},
		{BatchID: batchPending},
	}, nil)

	batchTrigger.EXPECT().ListTasksByBatchID(mock.Anything, batchDone).Return([]repo.Task{
		{ID: taskID, BatchID: batchDone, SourceType: PartySourceType, Status: "COMPLETED", TraceID: "trace-id-1"},
	}, nil)
	batchTrigger.EXPECT().CountCandidatesByBatchID(mock.Anything, batchDone).Return(int64(2), nil)
	batchTrigger.EXPECT().ListContentsByBatchID(mock.Anything, batchDone).Return([]repo.Content{
		{BatchID: batchDone}, {BatchID: batchDone},
	}, nil)

	batchTrigger.EXPECT().ListTasksByBatchID(mock.Anything, batchPending).Return([]repo.Task{
		{ID: uuid.Must(uuid.NewV7()), BatchID: batchPending, SourceType: PartySourceType, Status: "RUNNING", TraceID: "trace-b"},
	}, nil)
	batchTrigger.EXPECT().CountCandidatesByBatchID(mock.Anything, batchPending).Return(int64(2), nil)
	batchTrigger.EXPECT().ListContentsByBatchID(mock.Anything, batchPending).Return([]repo.Content{
		{BatchID: batchPending},
	}, nil)

	got, err := tr.ScanCompletedBatches(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, batchDone, got[0].BatchID)
	require.Equal(t, "trace-id-1", got[0].TraceID)
	require.Equal(t, int64(2), got[0].CandidateCount)
	require.Equal(t, 2, got[0].ContentCount)
	require.Equal(t, []uuid.UUID{taskID}, got[0].CompletedTaskIDs)
}

func TestScanCompletedBatchesSkipsIncompleteCandidatePromotion(t *testing.T) {
	batchTrigger := repomocks.NewMockBatchTrigger(t)
	tr, err := New(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), batchTrigger)
	require.NoError(t, err)

	batchID := uuid.Must(uuid.NewV7())
	batchTrigger.EXPECT().ListRecentSeedContents(mock.Anything, int32(10)).Return([]repo.Content{
		{BatchID: batchID},
	}, nil)
	batchTrigger.EXPECT().ListTasksByBatchID(mock.Anything, batchID).Return([]repo.Task{
		{ID: uuid.Must(uuid.NewV7()), BatchID: batchID, SourceType: PartySourceType, Status: "COMPLETED", TraceID: "trace-a"},
		{ID: uuid.Must(uuid.NewV7()), BatchID: batchID, SourceType: PartySourceType, Status: "COMPLETED", TraceID: "trace-b"}, // different trace, trace-a should win
	}, nil)
	batchTrigger.EXPECT().CountCandidatesByBatchID(mock.Anything, batchID).Return(int64(3), nil)
	batchTrigger.EXPECT().ListContentsByBatchID(mock.Anything, batchID).Return([]repo.Content{
		{BatchID: batchID}, {BatchID: batchID},
	}, nil)

	got, err := tr.ScanCompletedBatches(context.Background(), 10)
	require.NoError(t, err)
	require.Empty(t, got)
}
