package pg

import (
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBTaskToRepoTask_ConvertsNullableFields(t *testing.T) {
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	lastRun := now.Add(time.Minute)
	id := uuid.New()
	batchID := uuid.New()
	payloadHash := "hash-123"

	got := dbTaskToRepoTask(Task{
		ID:          id,
		BatchID:     batchID,
		Kind:        TaskKindPAGEFETCH,
		SourceType:  SourceTypePARTY,
		SourceAbbr:  "dpp",
		Url:         "https://example.test/article",
		Payload:     []byte(`{"url":"https://example.test/article"}`),
		PayloadHash: pgtype.Text{String: payloadHash, Valid: true},
		Meta:        []byte(`{"candidate_id":"abc"}`),
		TraceID:     "trace-123",
		NextRunAt:   pgtype.Timestamptz{Time: now, Valid: true},
		ExpiresAt:   pgtype.Timestamptz{},
		Status:      TaskStatusRUNNING,
		RetryCount:  2,
		LastRunAt:   pgtype.Timestamptz{Time: lastRun, Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: now.Add(-time.Minute), Valid: true},
	})

	assert.Equal(t, id, got.ID)
	assert.Equal(t, batchID, got.BatchID)
	assert.Equal(t, repo.TaskKindPageFetch, got.Kind)
	assert.Equal(t, repo.SourceTypeParty, got.SourceType)
	assert.Equal(t, "dpp", got.SourceAbbr)
	assert.Equal(t, "https://example.test/article", got.URL)
	require.NotNil(t, got.PayloadHash)
	assert.Equal(t, payloadHash, *got.PayloadHash)
	assert.Nil(t, got.ExpiresAt)
	assert.Equal(t, repo.TaskStatusRunning, got.Status)
	assert.Equal(t, 2, got.RetryCount)
	require.NotNil(t, got.LastRunAt)
	assert.Equal(t, lastRun, *got.LastRunAt)
}

func TestDBCandidateToRepoCandidate_ConvertsNullableFields(t *testing.T) {
	now := time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC)
	id := uuid.New()
	batchID := uuid.New()
	description := "brief"

	got := dbCandidateToRepoCandidate(Candidate{
		ID:              id,
		BatchID:         pgtype.UUID{Bytes: batchID, Valid: true},
		SourceAbbr:      "dpp",
		TraceID:         "trace-123",
		Fingerprint:     "fingerprint",
		Url:             "https://example.test/candidate",
		Title:           "Candidate Title",
		Description:     pgtype.Text{String: description, Valid: true},
		IngestionMethod: CandidateIngestionMethodDIRECTORY,
		Metadata:        []byte(`{"source":"fixture"}`),
		PublishedAt:     pgtype.Timestamptz{},
		DiscoveredAt:    pgtype.Timestamptz{Time: now, Valid: true},
		CreatedAt:       pgtype.Timestamptz{Time: now.Add(time.Second), Valid: true},
	})

	assert.Equal(t, id, got.ID)
	assert.Equal(t, batchID, got.BatchID)
	require.NotNil(t, got.Description)
	assert.Equal(t, description, *got.Description)
	assert.Nil(t, got.PublishedAt)
	assert.Equal(t, now, got.DiscoveredAt)
	assert.Equal(t, repo.IngestionMethodDirectory, got.IngestionMethod)
}

func TestDBContentToRepoContent_ConvertsNullableFields(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	id := uuid.New()
	batchID := uuid.New()
	candidateID := uuid.New()
	deletedAt := now.Add(time.Hour)

	got := dbContentToRepoContent(Content{
		ID:          id,
		BatchID:     pgtype.UUID{Bytes: batchID, Valid: true},
		Type:        ContentTypePARTYRELEASE,
		SourceAbbr:  "dpp",
		CandidateID: pgtype.UUID{Bytes: candidateID, Valid: true},
		Url:         "https://example.test/content",
		Title:       "Content Title",
		Content:     "body",
		Author:      pgtype.Text{},
		TraceID:     "trace-123",
		PublishedAt: pgtype.Timestamptz{Time: now, Valid: true},
		FetchedAt:   pgtype.Timestamptz{Time: now.Add(time.Minute), Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: now.Add(2 * time.Minute), Valid: true},
		DeletedAt:   pgtype.Timestamptz{Time: deletedAt, Valid: true},
		Metadata:    []byte(`{"recovered":true}`),
	})

	assert.Equal(t, id, got.ID)
	assert.Equal(t, batchID, got.BatchID)
	assert.Equal(t, candidateID, got.CandidateID)
	assert.Equal(t, string(ContentTypePARTYRELEASE), got.Type)
	assert.Nil(t, got.Author)
	require.NotNil(t, got.DeletedAt)
	assert.Equal(t, deletedAt, *got.DeletedAt)
}
