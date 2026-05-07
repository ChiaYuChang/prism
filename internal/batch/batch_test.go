package batch

import (
	"context"
	"errors"
	"testing"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

// fakeBatchCompletedPublisher records published signals and can be scripted
// to fail on the Nth call. The message package does not have generated
// mockery mocks, so we use a small hand-written fake here.
type fakeBatchCompletedPublisher struct {
	published []*message.BatchCompletedSignal
	errs      []error // pop front on each call; if empty, return nil
}

func (f *fakeBatchCompletedPublisher) PublishBatchCompleted(_ context.Context, sig *message.BatchCompletedSignal) error {
	f.published = append(f.published, sig)
	if len(f.errs) == 0 {
		return nil
	}
	err := f.errs[0]
	f.errs = f.errs[1:]
	return err
}

func TestDetector_Detect(t *testing.T) {
	batchID := uuid.Must(uuid.NewV7())
	traceID := "trace-123"
	limit := int32(10)

	mRepo := mocks.NewMockBatchTrigger(t)
	mRepo.EXPECT().
		FindNewlyCompletedBatches(mock.Anything, limit, repo.SourceTypeParty).
		Return([]repo.Batch{
			{ID: batchID, SourceType: repo.SourceTypeParty, TraceID: &traceID},
		}, nil)

	mRepo.EXPECT().
		MarkBatchCompleted(mock.Anything, batchID, traceID).
		Return(int64(1), nil)

	d, err := NewDetector(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), mRepo)
	require.NoError(t, err)

	got, err := d.Detect(context.Background(), limit)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, batchID, got[0].BatchID)
}

// TestDetector_Detect_LoserDropsBatch verifies that when MarkBatchCompleted
// reports rows==0 (another instance won the OCC race) the detector silently
// drops the batch instead of returning it for publish, preventing duplicate
// batch.completed signals across multi-instance trigger deployments.
func TestDetector_Detect_LoserDropsBatch(t *testing.T) {
	batchID := uuid.Must(uuid.NewV7())
	traceID := "trace-loser"
	limit := int32(10)

	mRepo := mocks.NewMockBatchTrigger(t)
	mRepo.EXPECT().
		FindNewlyCompletedBatches(mock.Anything, limit, repo.SourceTypeParty).
		Return([]repo.Batch{
			{ID: batchID, SourceType: repo.SourceTypeParty, TraceID: &traceID},
		}, nil)
	mRepo.EXPECT().
		MarkBatchCompleted(mock.Anything, batchID, traceID).
		Return(int64(0), nil)

	d, err := NewDetector(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), mRepo)
	require.NoError(t, err)

	got, err := d.Detect(context.Background(), limit)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestGetBatchProgressTracksTaskIDsByStatus(t *testing.T) {
	batchID := uuid.Must(uuid.NewV7())
	pendingID := uuid.Must(uuid.NewV7())
	completedID := uuid.Must(uuid.NewV7())

	mRepo := mocks.NewMockBatchTrigger(t)
	mRepo.EXPECT().
		ListTasksByBatchID(mock.Anything, batchID).
		Return([]repo.Task{
			{
				ID:         pendingID,
				BatchID:    batchID,
				SourceType: repo.SourceTypeParty,
				Status:     repo.TaskStatusPending,
				TraceID:    "trace-a",
			},
			{
				ID:         completedID,
				BatchID:    batchID,
				SourceType: repo.SourceTypeParty,
				Status:     repo.TaskStatusCompleted,
				TraceID:    "trace-a",
			},
		}, nil)

	mRepo.EXPECT().
		CountCandidatesByBatchID(mock.Anything, batchID).
		Return(int64(1), nil)

	mRepo.EXPECT().
		ListContentsByBatchID(mock.Anything, batchID).
		Return([]repo.Content{{BatchID: batchID}}, nil)

	d, err := NewDetector(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), mRepo)
	require.NoError(t, err)

	progress, err := d.GetBatchProgress(context.Background(), batchID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{pendingID}, progress.TaskIDsByStatus[repo.TaskStatusPending])
	require.Equal(t, []uuid.UUID{completedID}, progress.TaskIDsByStatus[repo.TaskStatusCompleted])
}

func newPublisher(t *testing.T, r repo.BatchTrigger, p message.BatchCompletedPublisher) *Publisher {
	t.Helper()
	pub, err := NewPublisher(testutils.Logger(), noop.NewTracerProvider().Tracer("test"), r, p)
	require.NoError(t, err)
	return pub
}

func TestPublisher_Publish_Success(t *testing.T) {
	limit := int32(10)
	traceA, traceB := "trace-a", "trace-b"
	batchA := repo.Batch{ID: uuid.Must(uuid.NewV7()), SourceType: repo.SourceTypeParty, TraceID: &traceA}
	batchB := repo.Batch{ID: uuid.Must(uuid.NewV7()), SourceType: repo.SourceTypeParty, TraceID: &traceB}

	mRepo := mocks.NewMockBatchTrigger(t)
	mRepo.EXPECT().
		ListReadyToPublishBatches(mock.Anything, limit, repo.SourceTypeParty).
		Return([]repo.Batch{batchA, batchB}, nil)
	mRepo.EXPECT().MarkBatchPublished(mock.Anything, batchA.ID).Return(nil)
	mRepo.EXPECT().MarkBatchPublished(mock.Anything, batchB.ID).Return(nil)

	fakePub := &fakeBatchCompletedPublisher{}
	p := newPublisher(t, mRepo, fakePub)

	count, err := p.Publish(context.Background(), limit)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Len(t, fakePub.published, 2)
	require.Equal(t, batchA.ID, fakePub.published[0].BatchID)
	require.Equal(t, traceA, fakePub.published[0].TraceID)
	require.Equal(t, batchB.ID, fakePub.published[1].BatchID)
}

func TestPublisher_Publish_Empty(t *testing.T) {
	limit := int32(10)
	mRepo := mocks.NewMockBatchTrigger(t)
	mRepo.EXPECT().
		ListReadyToPublishBatches(mock.Anything, limit, repo.SourceTypeParty).
		Return([]repo.Batch{}, nil)

	fakePub := &fakeBatchCompletedPublisher{}
	p := newPublisher(t, mRepo, fakePub)

	count, err := p.Publish(context.Background(), limit)
	require.NoError(t, err)
	require.Equal(t, 0, count)
	require.Empty(t, fakePub.published)
}

func TestPublisher_Publish_MQFailure_RecordsAndContinues(t *testing.T) {
	limit := int32(10)
	traceA, traceB := "trace-a", "trace-b"
	batchA := repo.Batch{ID: uuid.Must(uuid.NewV7()), SourceType: repo.SourceTypeParty, TraceID: &traceA}
	batchB := repo.Batch{ID: uuid.Must(uuid.NewV7()), SourceType: repo.SourceTypeParty, TraceID: &traceB}

	mqErr := errors.New("nats connection reset")

	mRepo := mocks.NewMockBatchTrigger(t)
	mRepo.EXPECT().
		ListReadyToPublishBatches(mock.Anything, limit, repo.SourceTypeParty).
		Return([]repo.Batch{batchA, batchB}, nil)
	// Batch A's publish fails → failure is recorded, loop continues.
	mRepo.EXPECT().RecordBatchPublishFailure(mock.Anything, batchA.ID, mqErr.Error()).Return(nil)
	// Batch B's publish succeeds → marked published.
	mRepo.EXPECT().MarkBatchPublished(mock.Anything, batchB.ID).Return(nil)

	fakePub := &fakeBatchCompletedPublisher{errs: []error{mqErr}} // first call fails, second succeeds
	p := newPublisher(t, mRepo, fakePub)

	count, err := p.Publish(context.Background(), limit)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Len(t, fakePub.published, 2) // both attempted
}

func TestPublisher_Publish_RecordFailureError_Aborts(t *testing.T) {
	limit := int32(10)
	traceA := "trace-a"
	batchA := repo.Batch{ID: uuid.Must(uuid.NewV7()), SourceType: repo.SourceTypeParty, TraceID: &traceA}

	mqErr := errors.New("nats timeout")
	dbErr := errors.New("db unavailable")

	mRepo := mocks.NewMockBatchTrigger(t)
	mRepo.EXPECT().
		ListReadyToPublishBatches(mock.Anything, limit, repo.SourceTypeParty).
		Return([]repo.Batch{batchA}, nil)
	mRepo.EXPECT().RecordBatchPublishFailure(mock.Anything, batchA.ID, mqErr.Error()).Return(dbErr)

	fakePub := &fakeBatchCompletedPublisher{errs: []error{mqErr}}
	p := newPublisher(t, mRepo, fakePub)

	count, err := p.Publish(context.Background(), limit)
	require.Error(t, err)
	require.ErrorIs(t, err, mqErr)
	require.ErrorIs(t, err, dbErr)
	require.Equal(t, 0, count)
}

func TestPublisher_Publish_MarkPublishedError_Aborts(t *testing.T) {
	limit := int32(10)
	traceA, traceB := "trace-a", "trace-b"
	batchA := repo.Batch{ID: uuid.Must(uuid.NewV7()), SourceType: repo.SourceTypeParty, TraceID: &traceA}
	batchB := repo.Batch{ID: uuid.Must(uuid.NewV7()), SourceType: repo.SourceTypeParty, TraceID: &traceB}

	dbErr := errors.New("db write failed")

	mRepo := mocks.NewMockBatchTrigger(t)
	mRepo.EXPECT().
		ListReadyToPublishBatches(mock.Anything, limit, repo.SourceTypeParty).
		Return([]repo.Batch{batchA, batchB}, nil)
	mRepo.EXPECT().MarkBatchPublished(mock.Anything, batchA.ID).Return(dbErr)
	// batchB is never reached because Publish short-circuits on mark error.

	fakePub := &fakeBatchCompletedPublisher{}
	p := newPublisher(t, mRepo, fakePub)

	count, err := p.Publish(context.Background(), limit)
	require.Error(t, err)
	require.ErrorIs(t, err, dbErr)
	require.Equal(t, 0, count)
	require.Len(t, fakePub.published, 1) // only batch A was attempted
}

func TestPublisher_Publish_ListError(t *testing.T) {
	limit := int32(10)
	dbErr := errors.New("query failed")

	mRepo := mocks.NewMockBatchTrigger(t)
	mRepo.EXPECT().
		ListReadyToPublishBatches(mock.Anything, limit, repo.SourceTypeParty).
		Return(nil, dbErr)

	fakePub := &fakeBatchCompletedPublisher{}
	p := newPublisher(t, mRepo, fakePub)

	count, err := p.Publish(context.Background(), limit)
	require.Error(t, err)
	require.ErrorIs(t, err, dbErr)
	require.Equal(t, 0, count)
	require.Empty(t, fakePub.published)
}

func TestNewPublisher_ValidatesDeps(t *testing.T) {
	mRepo := mocks.NewMockBatchTrigger(t)
	fakePub := &fakeBatchCompletedPublisher{}
	tracer := noop.NewTracerProvider().Tracer("test")
	logger := testutils.Logger()

	_, err := NewPublisher(nil, tracer, mRepo, fakePub)
	require.ErrorIs(t, err, ErrParamMissing)
	_, err = NewPublisher(logger, nil, mRepo, fakePub)
	require.ErrorIs(t, err, ErrParamMissing)
	_, err = NewPublisher(logger, tracer, nil, fakePub)
	require.ErrorIs(t, err, ErrParamMissing)
	_, err = NewPublisher(logger, tracer, mRepo, nil)
	require.ErrorIs(t, err, ErrParamMissing)
}
