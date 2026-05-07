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
	FetchID         uuid.UUID `json:"fetch_id"`
	Total           int64     `json:"total"`
	Pending         int64     `json:"pending"`
	Running         int64     `json:"running"`
	Completed       int64     `json:"completed"`
	Failed          int64     `json:"failed"`
	AlreadyComplete int64     `json:"already_complete"`
	Terminal        bool      `json:"terminal"`
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

	writeJSON(w, http.StatusOK, FetchProgressResponse{
		FetchID:         fetchID,
		Total:           progress.Total,
		Pending:         progress.Pending,
		Running:         progress.Running,
		Completed:       progress.Completed,
		Failed:          progress.Failed,
		AlreadyComplete: progress.AlreadyComplete,
		Terminal:        progress.Terminal,
	})
}
