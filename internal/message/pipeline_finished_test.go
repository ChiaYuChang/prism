package message_test

import (
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/message"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPipelineBatchFinishedSignalRoundTrip(t *testing.T) {
	sig := &message.PipelineBatchFinishedSignal{
		BatchID:     uuid.New(),
		RootBatchID: uuid.New(),
		OwnerTaskID: uuid.New(),
		Succeeded:   true,
		TraceID:     "trace",
		SentAt:      time.Now().UTC().Truncate(time.Microsecond),
	}
	payload, err := sig.Marshal()
	require.NoError(t, err)
	var got message.PipelineBatchFinishedSignal
	require.NoError(t, got.Unmarshal(payload))
	require.Equal(t, *sig, got)
}
