package sink

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestApplyRequestDefaults(t *testing.T) {
	reqBatchID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	candBatchID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	publishedAt := time.Date(2026, 4, 4, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		req       CandidateSinkRequest
		candidate model.Candidates
		wantErr   error
		assert    func(t *testing.T, got model.Candidates)
	}{
		{
			name: "fallback to request defaults",
			req: CandidateSinkRequest{
				SourceAbbr:      "dpp",
				TraceID:         "req-trace",
				BatchID:         reqBatchID,
				IngestionMethod: repo.IngestionMethodDirectory,
				DefaultMetadata: map[string]any{"req-meta": "value1"},
			},
			candidate: model.Candidates{
				URL:         "https://example.com/a",
				Title:       "Example",
				Description: "Desc",
				PublishedAt: publishedAt,
				Metadata:    map[string]any{"cand-meta": "value2"},
			},
			wantErr: nil,
			assert: func(t *testing.T, got model.Candidates) {
				require.Equal(t, "dpp", got.SourceAbbr)
				require.Equal(t, "req-trace", got.TraceID)
				require.Equal(t, reqBatchID, got.BatchID)
				require.Equal(t, repo.IngestionMethodDirectory, got.IngestionMethod)

				require.Equal(t, "value1", got.Metadata["req-meta"])
				require.Equal(t, "value2", got.Metadata["cand-meta"])
			},
		},
		{
			name: "overrides using candidate values",
			req: CandidateSinkRequest{
				SourceAbbr:      "dpp",
				TraceID:         "req-trace",
				BatchID:         reqBatchID,
				IngestionMethod: repo.IngestionMethodDirectory,
			},
			candidate: model.Candidates{
				SourceAbbr:      "kmt",
				TraceID:         "cand-trace",
				BatchID:         candBatchID,
				IngestionMethod: repo.IngestionMethodSearch,
				URL:             "https://example.com/b",
				Title:           "Override",
			},
			wantErr: nil,
			assert: func(t *testing.T, got model.Candidates) {
				require.Equal(t, "kmt", got.SourceAbbr)
				require.Equal(t, "cand-trace", got.TraceID)
				require.Equal(t, candBatchID, got.BatchID)
				require.Equal(t, repo.IngestionMethodSearch, got.IngestionMethod)
			},
		},
		{
			name: "missing source abbr",
			req: CandidateSinkRequest{
				TraceID: "req-trace",
			},
			candidate: model.Candidates{
				URL: "https://example.com/c",
			},
			wantErr: ErrMissingSourceID,
		},
		{
			name: "missing trace ID",
			req: CandidateSinkRequest{
				SourceAbbr: "dpp",
			},
			candidate: model.Candidates{
				URL: "https://example.com/d",
			},
			wantErr: ErrMissingTraceID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyRequestDefaults(tt.candidate, tt.req)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)

			if tt.assert != nil {
				tt.assert(t, got)
			}
		})
	}
}

func TestToUpsertCandidateParams(t *testing.T) {
	batchID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	publishedAt := time.Date(2026, 4, 4, 9, 0, 0, 0, time.UTC)

	candidate := model.Candidates{
		BatchID:         batchID,
		SourceAbbr:      "pts",
		TraceID:         "some-trace",
		IngestionMethod: repo.IngestionMethodDirectory,
		URL:             "https://example.com/test",
		Title:           "Test Title",
		Description:     "Test Description",
		PublishedAt:     publishedAt,
		Metadata:        map[string]any{"key": "value"},
	}

	got, err := toUpsertCandidateParams(candidate)
	require.NoError(t, err)

	require.Equal(t, batchID, got.BatchID)
	require.Equal(t, "pts", got.SourceAbbr)
	require.Equal(t, "some-trace", got.TraceID)
	require.Equal(t, repo.IngestionMethodDirectory, got.IngestionMethod)
	require.Equal(t, "https://example.com/test", got.URL)
	require.Equal(t, "Test Title", got.Title)

	require.NotNil(t, got.Description)
	require.Equal(t, "Test Description", *got.Description)

	require.NotNil(t, got.PublishedAt)
	require.Equal(t, publishedAt, *got.PublishedAt)

	require.Equal(t, candidate.Fingerprint(), got.Fingerprint)

	var meta map[string]any
	err = json.Unmarshal(got.Metadata, &meta)
	require.NoError(t, err)
	require.Equal(t, "value", meta["key"])
}
