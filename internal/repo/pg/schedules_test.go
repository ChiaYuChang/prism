package pg

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSchedulePayloadHashIncludesConfigHash(t *testing.T) {
	id := uuid.MustParse("019f23a0-0000-7000-8000-000000000001")

	first := schedulePayloadHash(id, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	second := schedulePayloadHash(id, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	require.Len(t, first, 64)
	require.NotEqual(t, first, second)
}
