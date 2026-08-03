package pipeline

import (
	"testing"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRegistryBuildsDeterministicEmbeddingWork(t *testing.T) {
	candidateA := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	candidateB := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	registry, err := NewRegistry(EmbedCandidateBuilder{}, EmbedContentBuilder{})
	require.NoError(t, err)

	tasks, err := registry.Build("embed", map[string]any{
		"task_type": repo.TaskKindEmbedCandidate,
		"fan_out":   "candidate_ids",
	}, WorkSetInput{
		CandidateIDs:        []uuid.UUID{candidateA, candidateB},
		CandidateSourceAbbr: map[uuid.UUID]string{candidateA: "dpp", candidateB: "dpp"},
	})
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	require.Equal(t, candidateB.String(), tasks[0].LogicalKey[len("embed:"):])
	require.Equal(t, repo.TaskKindEmbedCandidate, tasks[0].Kind)
}

func TestRegistryRejectsUnsupportedOrInvalidConfig(t *testing.T) {
	registry, err := NewRegistry(EmbedCandidateBuilder{})
	require.NoError(t, err)
	_, err = registry.Build("stage", map[string]any{"task_type": "UNKNOWN"}, WorkSetInput{})
	require.Error(t, err)
	_, err = registry.Build("stage", map[string]any{"task_type": repo.TaskKindEmbedCandidate, "fan_out": "content_ids"}, WorkSetInput{})
	require.Error(t, err)
}
