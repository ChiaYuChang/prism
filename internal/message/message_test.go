package message_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/ChiaYuChang/prism/pkg/archivecodec"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTaskSignal_RoundTrip(t *testing.T) {
	taskID := uuid.New()
	batchID := uuid.New()
	sentAt := time.Now().Truncate(time.Second) // JSON typical precision

	sig := message.TaskSignal{
		TaskID:     taskID,
		BatchID:    batchID,
		Kind:       "PAGE_FETCH",
		SourceType: "MEDIA",
		SourceAbbr: "yahoo",
		URL:        "https://example.com",
		Payload:    json.RawMessage(`{"query":"test"}`),
		Meta:       json.RawMessage(`{"candidate_id":"` + uuid.NewString() + `"}`),
		TraceID:    "trace-123",
		SentAt:     sentAt,
	}

	data, err := sig.Marshal()
	require.NoError(t, err)

	var got message.TaskSignal
	err = got.Unmarshal(data)
	require.NoError(t, err)

	require.Equal(t, sig.TaskID, got.TaskID)
	require.Equal(t, sig.BatchID, got.BatchID)
	require.Equal(t, sig.Kind, got.Kind)
	require.Equal(t, sig.SourceType, got.SourceType)
	require.Equal(t, sig.SourceAbbr, got.SourceAbbr)
	require.Equal(t, sig.URL, got.URL)
	require.JSONEq(t, string(sig.Payload), string(got.Payload))
	require.JSONEq(t, string(sig.Meta), string(got.Meta))
	require.Equal(t, sig.TraceID, got.TraceID)
	require.True(t, sig.SentAt.Equal(got.SentAt))
}

func TestBatchCompletedSignal_RoundTrip(t *testing.T) {
	batchID := uuid.New()
	sentAt := time.Now().Truncate(time.Second)

	sig := message.BatchCompletedSignal{
		BatchID:    batchID,
		SourceType: "PARTY",
		TraceID:    "trace-456",
		SentAt:     sentAt,
	}

	data, err := sig.Marshal()
	require.NoError(t, err)

	var got message.BatchCompletedSignal
	err = got.Unmarshal(data)
	require.NoError(t, err)

	require.Equal(t, sig.BatchID, got.BatchID)
	require.Equal(t, sig.SourceType, got.SourceType)
	require.Equal(t, sig.TraceID, got.TraceID)
	require.True(t, sig.SentAt.Equal(got.SentAt))
}

func TestArchiveSignal_RoundTrip(t *testing.T) {
	contentID := uuid.New()
	fetchedAt := time.Now().Truncate(time.Second)

	sig := message.ArchiveSignal{
		ContentID: contentID,
		URL:       "https://example.com/article",
		TraceID:   "trace-789",
		FetchedAt: fetchedAt,
		Page: archivecodec.Blob{
			OriginalSize:      100,
			CompressionMethod: archivecodec.CompressionGzip,
			Encoding:          archivecodec.EncodingBase64,
			Content:           "compressed-data",
		},
	}

	data, err := json.Marshal(sig)
	require.NoError(t, err)

	var got message.ArchiveSignal
	err = json.Unmarshal(data, &got)
	require.NoError(t, err)

	require.Equal(t, sig.ContentID, got.ContentID)
	require.Equal(t, sig.URL, got.URL)
	require.Equal(t, sig.TraceID, got.TraceID)
	require.True(t, sig.FetchedAt.Equal(got.FetchedAt))
	require.Equal(t, sig.Page.OriginalSize, got.Page.OriginalSize)
	require.Equal(t, sig.Page.CompressionMethod, got.Page.CompressionMethod)
	require.Equal(t, sig.Page.Encoding, got.Page.Encoding)
	require.Equal(t, sig.Page.Content, got.Page.Content)
}
