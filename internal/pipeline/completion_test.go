package pipeline

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompletionRequiresDeclaredExactCardinality(t *testing.T) {
	expected := int32(2)
	require.False(t, (Completion{Expected: nil, Actual: 2, Terminal: 2}).Finished())
	require.False(t, (Completion{Expected: &expected, Actual: 1, Terminal: 1}).Finished())
	require.False(t, (Completion{Expected: &expected, Actual: 2, Terminal: 1}).Finished())
	require.True(t, (Completion{Expected: &expected, Actual: 2, Terminal: 2}).Finished())
}

func TestCompletionSeparatesFinishedFromSucceeded(t *testing.T) {
	expected := int32(2)
	finishedFailed := Completion{Expected: &expected, Actual: 2, Terminal: 2, Failed: 1}
	finishedCancelled := Completion{Expected: &expected, Actual: 2, Terminal: 2, Cancelled: 1}
	require.True(t, finishedFailed.Finished())
	require.False(t, finishedFailed.Succeeded())
	require.False(t, finishedCancelled.Succeeded())
}
