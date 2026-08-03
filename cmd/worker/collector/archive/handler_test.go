package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/pkg/archivecodec"
	wm "github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHandlerHandleMessage_SavesCanonicalContentByContentID(t *testing.T) {
	page, err := archivecodec.GzipBase64.PackString("<html>canonical</html>")
	require.NoError(t, err)
	contentID := uuid.Must(uuid.NewV7())
	fetchedAt := time.Now().UTC().Round(0)
	payload, err := json.Marshal(message.ArchiveSignal{
		ContentID: contentID,
		URL:       "https://example.com/article",
		TraceID:   "shared-trace-id",
		FetchedAt: fetchedAt,
		Page:      *page,
	})
	require.NoError(t, err)
	saver := &capturingSaver{}
	handler, err := NewHandler(saver)
	require.NoError(t, err)

	ack, err := handler.HandleMessage(context.Background(), wm.NewMessage(uuid.NewString(), payload))
	require.NoError(t, err)
	require.True(t, ack)
	require.Equal(t, contentID.String(), saver.record.ID)
	require.Equal(t, "<html>canonical</html>", saver.record.Payload)
	require.Equal(t, "canonical", saver.record.Metadata["kind"])
}

func TestHandlerHandleMessage_InvalidPayloadIsAcked(t *testing.T) {
	handler, err := NewHandler(&capturingSaver{})
	require.NoError(t, err)

	ack, err := handler.HandleMessage(context.Background(), wm.NewMessage(uuid.NewString(), []byte("not json")))
	require.ErrorIs(t, err, ErrInvalidArchiveSignal)
	require.True(t, ack)
}

type capturingSaver struct {
	record collector.Archive
}

func (s *capturingSaver) Save(_ context.Context, record collector.Archive) error {
	s.record = record
	return nil
}
