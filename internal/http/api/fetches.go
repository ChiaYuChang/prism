package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// FetchProgressResponse is returned by GET /api/v1/fetches/{id}.
//
// Counts cover only persisted items, so `not_found` candidates from the
// originating POST never appear here. Aggregation uses
// COALESCE(snapshot_status, tasks.status) per item.
type FetchProgressResponse struct {
	FetchID         uuid.UUID           `json:"fetch_id"`
	Total           int64               `json:"total"`
	Pending         FetchProgressStatus `json:"pending"`
	Running         FetchProgressStatus `json:"running"`
	Completed       FetchProgressStatus `json:"completed"`
	Failed          FetchProgressStatus `json:"failed"`
	AlreadyComplete FetchProgressStatus `json:"already_complete"`
	Terminal        bool                `json:"terminal"`
}

// FetchProgressStatus groups candidate IDs for one resolved fetch status.
type FetchProgressStatus struct {
	Count        int         `json:"count"`
	CandidateIDs []uuid.UUID `json:"candidate_ids"`
}

// GetFetch handles GET /api/v1/fetches/{id}.
//
// Returns aggregated progress for one user fetch. The caller never sees
// per-item task_ids or membership of other fetches.
//
// @Summary   Get progress for a user fetch
// @Tags      fetches
// @Produce   json
// @Param     id path string true "User fetch ID"
// @Success   200 {object} FetchProgressResponse
// @Failure   400 {object} ErrorResponse
// @Failure   404 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /fetches/{id} [get]
func (s *Server) GetFetch(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("id")
	fetchID, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid fetch id")
		return
	}

	ctx := r.Context()

	if cached, ok, err := s.Cache.Get(ctx, fetchID); err != nil {
		s.Logger.WarnContext(ctx, "progress cache get failed",
			slog.String("fetch_id", fetchID.String()), slog.Any("error", err))
	} else if ok {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	if _, err := s.UserFetches.Get(ctx, fetchID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "fetch not found")
			return
		}
		s.Logger.ErrorContext(ctx, "get user fetch failed",
			slog.String("fetch_id", fetchID.String()), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to load fetch")
		return
	}

	progress, err := s.UserFetches.GetProgress(ctx, fetchID)
	if err != nil {
		s.Logger.ErrorContext(ctx, "get user fetch progress failed",
			slog.String("fetch_id", fetchID.String()), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to compute fetch progress")
		return
	}

	resp := FetchProgressResponse{
		FetchID:         fetchID,
		Total:           progress.Total,
		Pending:         fetchProgressStatus(progress.PendingCandidateIDs),
		Running:         fetchProgressStatus(progress.RunningCandidateIDs),
		Completed:       fetchProgressStatus(progress.CompletedCandidateIDs),
		Failed:          fetchProgressStatus(progress.FailedCandidateIDs),
		AlreadyComplete: fetchProgressStatus(progress.AlreadyCompleteCandidateIDs),
		Terminal:        progress.Terminal,
	}
	if err := s.Cache.Set(ctx, fetchID, resp); err != nil {
		s.Logger.WarnContext(ctx, "progress cache set failed",
			slog.String("fetch_id", fetchID.String()), slog.Any("error", err))
	}
	writeJSON(w, http.StatusOK, resp)
}

func fetchProgressStatus(candidateIDs []uuid.UUID) FetchProgressStatus {
	if candidateIDs == nil {
		candidateIDs = []uuid.UUID{}
	}
	return FetchProgressStatus{Count: len(candidateIDs), CandidateIDs: candidateIDs}
}
