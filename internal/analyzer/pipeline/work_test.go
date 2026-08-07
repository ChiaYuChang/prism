package pipeline

import (
	"encoding/json"
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
		CandidateSnapshots: map[uuid.UUID]repo.Candidate{
			candidateA: {ID: candidateA, SourceAbbr: "dpp", Title: "A", Metadata: []byte(`{"key":"a"}`)},
			candidateB: {ID: candidateB, SourceAbbr: "dpp", Title: "B", Metadata: []byte(`{"key":"b"}`)},
		},
	})
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	require.Equal(t, candidateB.String(), tasks[0].LogicalKey[len("embed:"):])
	require.Equal(t, repo.TaskKindEmbedCandidate, tasks[0].Kind)
	var meta struct {
		CandidateID string         `json:"candidate_id"`
		Snapshot    repo.Candidate `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(tasks[0].Meta, &meta))
	require.Equal(t, candidateB.String(), meta.CandidateID)
	require.Equal(t, "B", meta.Snapshot.Title)
	require.JSONEq(t, `{"key":"b"}`, string(meta.Snapshot.Metadata))
}

func TestRegistryRejectsUnsupportedOrInvalidConfig(t *testing.T) {
	registry, err := NewRegistry(EmbedCandidateBuilder{})
	require.NoError(t, err)
	_, err = registry.Build("stage", map[string]any{"task_type": "UNKNOWN"}, WorkSetInput{})
	require.Error(t, err)
	_, err = registry.Build("stage", map[string]any{"task_type": repo.TaskKindEmbedCandidate, "fan_out": "content_ids"}, WorkSetInput{})
	require.Error(t, err)
}
