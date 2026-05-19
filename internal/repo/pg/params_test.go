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

func TestRepoListCandidatesParamsToDB(t *testing.T) {
	query := "energy"
	sourceAbbr := "dpp"
	since := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)

	got := repoListCandidatesParamsToDB(repo.ListCandidatesParams{
		Query:      &query,
		SourceAbbr: &sourceAbbr,
		Since:      &since,
		Until:      &until,
		Limit:      25,
		Offset:     50,
	})

	assert.Equal(t, pgtype.Text{String: query, Valid: true}, got.Query)
	assert.Equal(t, pgtype.Text{String: sourceAbbr, Valid: true}, got.SourceAbbr)
	assert.Equal(t, pgtype.Timestamptz{Time: since, Valid: true}, got.Since)
	assert.Equal(t, pgtype.Timestamptz{Time: until, Valid: true}, got.Until)
	assert.Equal(t, int32(25), got.Lim)
	assert.Equal(t, int32(50), got.Off)

	empty := repoListCandidatesParamsToDB(repo.ListCandidatesParams{})
	assert.False(t, empty.Query.Valid)
	assert.False(t, empty.SourceAbbr.Valid)
	assert.False(t, empty.Since.Valid)
	assert.False(t, empty.Until.Valid)
}

func TestRepoCreateTaskParamsToDB(t *testing.T) {
	batchID := uuid.New()
	payloadHash := "payload-hash"
	frequency := 15 * time.Minute
	nextRunAt := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	expiresAt := nextRunAt.Add(time.Hour)

	arg := repo.CreateTaskParams{
		BatchID:     batchID,
		Kind:        repo.TaskKindPageFetch,
		SourceType:  repo.SourceTypeParty,
		SourceAbbr:  "dpp",
		URL:         "https://example.test/article",
		Payload:     []byte(`{"url":"https://example.test/article"}`),
		PayloadHash: &payloadHash,
		Meta:        []byte(`{"candidate_id":"abc"}`),
		TraceID:     "trace-123",
		Frequency:   &frequency,
		NextRunAt:   &nextRunAt,
		ExpiresAt:   &expiresAt,
	}

	ensure := repoCreateTaskParamsToEnsureBatchExists(arg)
	assert.Equal(t, batchID, ensure.ID)
	assert.Equal(t, SourceTypePARTY, ensure.SourceType)
	assert.Equal(t, pgtype.Text{String: "trace-123", Valid: true}, ensure.TraceID)

	got := repoCreateTaskParamsToDB(arg)
	assert.Equal(t, batchID, got.BatchID)
	assert.Equal(t, TaskKindPAGEFETCH, got.Kind)
	assert.Equal(t, SourceTypePARTY, got.SourceType)
	assert.Equal(t, "dpp", got.SourceAbbr)
	assert.Equal(t, "https://example.test/article", got.Url)
	assert.Equal(t, []byte(`{"url":"https://example.test/article"}`), got.Payload)
	assert.Equal(t, pgtype.Text{String: payloadHash, Valid: true}, got.PayloadHash)
	assert.Equal(t, int64(frequency/time.Microsecond), got.Frequency.Microseconds)
	assert.True(t, got.Frequency.Valid)
	require.IsType(t, pgtype.Timestamptz{}, got.NextRunAt)
	assert.Equal(t, pgtype.Timestamptz{Time: nextRunAt, Valid: true}, got.NextRunAt)
	assert.Equal(t, pgtype.Timestamptz{Time: expiresAt, Valid: true}, got.ExpiresAt)
}

func TestRepoCreateContentParamsToDB(t *testing.T) {
	batchID := uuid.New()
	candidateID := uuid.New()
	author := "Reporter"
	publishedAt := time.Date(2026, 5, 20, 11, 0, 0, 0, time.UTC)
	fetchedAt := publishedAt.Add(time.Minute)

	got := repoCreateContentParamsToDB(repo.CreateContentParams{
		BatchID:     batchID,
		Type:        "PARTY_RELEASE",
		SourceAbbr:  "dpp",
		CandidateID: candidateID,
		URL:         "https://example.test/content",
		Title:       "Title",
		Content:     "Body",
		Author:      &author,
		TraceID:     "trace-123",
		PublishedAt: publishedAt,
		FetchedAt:   fetchedAt,
		Metadata:    []byte(`{"recovered":true}`),
	})

	assert.Equal(t, pgtype.UUID{Bytes: batchID, Valid: true}, got.BatchID)
	assert.Equal(t, ContentTypePARTYRELEASE, got.Type)
	assert.Equal(t, pgtype.UUID{Bytes: candidateID, Valid: true}, got.CandidateID)
	assert.Equal(t, pgtype.Text{String: author, Valid: true}, got.Author)
	assert.Equal(t, pgtype.Timestamptz{Time: publishedAt, Valid: true}, got.PublishedAt)
	assert.Equal(t, pgtype.Timestamptz{Time: fetchedAt, Valid: true}, got.FetchedAt)
	assert.Equal(t, []byte(`{"recovered":true}`), got.Metadata)

	empty := repoCreateContentParamsToDB(repo.CreateContentParams{})
	assert.False(t, empty.BatchID.Valid)
	assert.False(t, empty.CandidateID.Valid)
	assert.False(t, empty.Author.Valid)
}
