package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/internal/repo"
	repomocks "github.com/ChiaYuChang/prism/internal/repo/mocks"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type stubTaskPublisher struct {
	publish func(topic string, messages ...*wm.Message) error
}

func (s stubTaskPublisher) Publish(topic string, messages ...*wm.Message) error {
	if s.publish != nil {
		return s.publish(topic, messages...)
	}
	return nil
}

func TestDispatchTasksPublishesTaskSignal(t *testing.T) {
	scheduler := repomocks.NewMockScheduler(t)
	taskID := uuid.Must(uuid.NewV7())
	batchID := uuid.Must(uuid.NewV7())

	tasks := []repo.Task{{
		ID:         taskID,
		BatchID:    batchID,
		Kind:       "DIRECTORY_FETCH",
		SourceType: "PARTY",
		SourceID:   1,
		URL:        "https://example.com/listing",
		TraceID:    "trace-123",
	}}

	var gotTopic string
	var gotSig message.TaskSignal
	publisher := stubTaskPublisher{
		publish: func(topic string, messages ...*wm.Message) error {
			require.Len(t, messages, 1)
			gotTopic = topic
			require.Equal(t, "trace-123", messages[0].Metadata.Get("trace_id"))
			require.NoError(t, gotSig.Unmarshal(messages[0].Payload))
			return nil
		},
	}

	err := dispatchTasks(context.Background(), testSchedulerLogger(), publisher, scheduler, tasks)
	require.NoError(t, err)
	require.Equal(t, message.TaskTopic, gotTopic)
	require.Equal(t, taskID, gotSig.TaskID)
	require.Equal(t, batchID, gotSig.BatchID)
	require.Equal(t, "PARTY", gotSig.SourceType)
	require.Equal(t, int32(1), gotSig.SourceID)
}

func TestDispatchTasksMarksTaskFailedWhenPublishFails(t *testing.T) {
	scheduler := repomocks.NewMockScheduler(t)
	taskID := uuid.Must(uuid.NewV7())
	tasks := []repo.Task{{
		ID:         taskID,
		BatchID:    uuid.Must(uuid.NewV7()),
		Kind:       "DIRECTORY_FETCH",
		SourceType: "PARTY",
		SourceID:   1,
		URL:        "https://example.com/listing",
		TraceID:    "trace-123",
	}}
	scheduler.EXPECT().FailTask(mock.Anything, taskID).Return(nil)

	publisher := stubTaskPublisher{
		publish: func(topic string, messages ...*wm.Message) error {
			return errors.New("publish failed")
		},
	}

	err := dispatchTasks(context.Background(), testSchedulerLogger(), publisher, scheduler, tasks)
	require.NoError(t, err)
}

func testSchedulerLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
