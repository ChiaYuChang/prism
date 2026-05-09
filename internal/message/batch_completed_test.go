package message_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ThreeDotsLabs/watermill"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPubSub(t *testing.T) *gochannel.GoChannel {
	t.Helper()
	logger := watermill.NewSlogLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return gochannel.NewGoChannel(
		gochannel.Config{
			OutputChannelBuffer:            8,
			BlockPublishUntilSubscriberAck: true,
		},
		logger,
	)
}

func TestNewWatermillBatchCompletedPublisher_NilPublisher(t *testing.T) {
	_, err := message.NewWatermillBatchCompletedPublisher(nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, message.ErrNilPublisher)
}

func TestNewWatermillBatchCompletedPublisher_OK(t *testing.T) {
	pubSub := newTestPubSub(t)
	t.Cleanup(func() { _ = pubSub.Close() })

	pub, err := message.NewWatermillBatchCompletedPublisher(pubSub)
	require.NoError(t, err)
	require.NotNil(t, pub)
}

func TestPublishBatchCompleted_RoundTrip(t *testing.T) {
	pubSub := newTestPubSub(t)
	t.Cleanup(func() { _ = pubSub.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msgs, err := pubSub.Subscribe(ctx, message.BatchCompletedTopic)
	require.NoError(t, err)

	pub, err := message.NewWatermillBatchCompletedPublisher(pubSub)
	require.NoError(t, err)

	sig := &message.BatchCompletedSignal{
		BatchID:    uuid.New(),
		SourceType: "PARTY",
		TraceID:    "trace-publish-rt",
		SentAt:     time.Now().Truncate(time.Second),
	}

	publishDone := make(chan error, 1)
	go func() {
		publishDone <- pub.PublishBatchCompleted(ctx, sig)
	}()

	select {
	case got, ok := <-msgs:
		require.True(t, ok, "subscriber channel closed before message arrived")
		assert.Equal(t, sig.TraceID, got.Metadata.Get("trace_id"))

		var decoded message.BatchCompletedSignal
		require.NoError(t, decoded.Unmarshal(got.Payload))
		assert.Equal(t, sig.BatchID, decoded.BatchID)
		assert.Equal(t, sig.SourceType, decoded.SourceType)
		assert.Equal(t, sig.TraceID, decoded.TraceID)
		assert.True(t, sig.SentAt.Equal(decoded.SentAt))

		got.Ack()
	case <-ctx.Done():
		t.Fatalf("timeout waiting for message: %v", ctx.Err())
	}

	require.NoError(t, <-publishDone)
}

// fakePublisher returns a fixed error from Publish so the wrapper's error-
// wrap path can be exercised without spinning up a real broker.
type fakePublisher struct{ err error }

func (f *fakePublisher) Publish(_ string, _ ...*wm.Message) error { return f.err }
func (f *fakePublisher) Close() error                             { return nil }

func TestPublishBatchCompleted_PublisherError(t *testing.T) {
	sentinel := errors.New("broker unavailable")
	pub, err := message.NewWatermillBatchCompletedPublisher(&fakePublisher{err: sentinel})
	require.NoError(t, err)

	err = pub.PublishBatchCompleted(context.Background(), &message.BatchCompletedSignal{
		BatchID: uuid.New(),
		TraceID: "trace-fail",
		SentAt:  time.Now(),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.ErrorContains(t, err, "publish batch completed signal")
}
