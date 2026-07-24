package analyzer_test

import (
	"testing"

	"github.com/ChiaYuChang/prism/internal/analyzer"
	"github.com/stretchr/testify/require"
)

func TestAssignIssueIDs(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		res := analyzer.AssignIssueIDs(nil)
		require.Nil(t, res)
	})

	t.Run("assigns sequential major and minor IDs", func(t *testing.T) {
		input := []analyzer.Issue{
			{Type: analyzer.IssueTypeMajor, Name: "Issue A"},
			{Type: analyzer.IssueTypeMinor, Name: "Issue B"},
			{Type: analyzer.IssueTypeMajor, Name: "Issue C"},
			{Type: analyzer.IssueTypeMinor, Name: "Issue D"},
		}

		res := analyzer.AssignIssueIDs(input)
		require.Len(t, res, 4)
		require.Equal(t, "major_1", res[0].ID)
		require.Equal(t, "minor_1", res[1].ID)
		require.Equal(t, "major_2", res[2].ID)
		require.Equal(t, "minor_2", res[3].ID)
	})
}

func TestValidateStanceScore(t *testing.T) {
	t.Run("valid scores 1 to 5", func(t *testing.T) {
		for score := 1; score <= 5; score++ {
			err := analyzer.ValidateStanceScore(score)
			require.NoError(t, err)
		}
	})

	t.Run("invalid score low", func(t *testing.T) {
		err := analyzer.ValidateStanceScore(0)
		require.ErrorIs(t, err, analyzer.ErrInvalidScore)
	})

	t.Run("invalid score high", func(t *testing.T) {
		err := analyzer.ValidateStanceScore(6)
		require.ErrorIs(t, err, analyzer.ErrInvalidScore)
	})
}
