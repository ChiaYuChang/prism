package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

const maxPageFetchBatch = 100

type PageFetchRequest struct {
	CandidateIDs []uuid.UUID `json:"candidate_ids"`
}

type PageFetchTaskResult struct {
	CandidateID uuid.UUID `json:"candidate_id"`
	TaskID      uuid.UUID `json:"task_id,omitempty"`
	Status      string    `json:"status"`
	Reason      string    `json:"reason,omitempty"`
}

type PageFetchResponse struct {
	Results []PageFetchTaskResult `json:"results"`
}

// PageFetch handles POST /api/v1/page_fetch.
//
// Creates MEDIA + PAGE_FETCH tasks for the supplied candidate IDs.
// Duplicate active tasks (same URL pending/running) are reported as
// "already_active" rather than an error — the endpoint is idempotent.
//
// @Summary   Request full-article fetch for candidates
// @Tags      candidates
// @Accept    json
// @Produce   json
// @Param     body body PageFetchRequest true "Candidate IDs to promote to contents"
// @Success   202 {object} PageFetchResponse
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /page_fetch [post]
func (s *Server) PageFetch(w http.ResponseWriter, r *http.Request) {
	var req PageFetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.CandidateIDs) == 0 {
		writeError(w, http.StatusBadRequest, "candidate_ids is required")
		return
	}
	if len(req.CandidateIDs) > maxPageFetchBatch {
		writeError(w, http.StatusBadRequest, "too many candidate_ids (max 100)")
		return
	}

	ctx := r.Context()
	candidates, err := s.Scout.GetCandidatesByIDs(ctx, req.CandidateIDs)
	if err != nil {
		s.Logger.ErrorContext(ctx, "fetch candidates failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to look up candidates")
		return
	}

	byID := make(map[uuid.UUID]repo.Candidate, len(candidates))
	for _, c := range candidates {
		byID[c.ID] = c
	}

	results := make([]PageFetchTaskResult, 0, len(req.CandidateIDs))
	for _, id := range req.CandidateIDs {
		c, ok := byID[id]
		if !ok {
			results = append(results, PageFetchTaskResult{CandidateID: id, Status: "not_found"})
			continue
		}

		meta, err := json.Marshal(map[string]any{"candidate_id": c.ID.String()})
		if err != nil {
			results = append(results, PageFetchTaskResult{CandidateID: id, Status: "error", Reason: "marshal meta"})
			continue
		}

		task, err := s.Tasks.CreateTask(ctx, repo.CreateTaskParams{
			BatchID:    c.BatchID,
			Kind:       repo.TaskKindPageFetch,
			SourceType: repo.SourceTypeMedia,
			SourceAbbr: c.SourceAbbr,
			URL:        c.URL,
			Meta:       meta,
			TraceID:    c.TraceID,
		})
		switch {
		case errors.Is(err, repo.ErrTaskAlreadyActive):
			results = append(results, PageFetchTaskResult{CandidateID: id, Status: "already_active"})
		case err != nil:
			s.Logger.ErrorContext(ctx, "create page_fetch task failed",
				slog.String("candidate_id", id.String()), slog.Any("error", err))
			results = append(results, PageFetchTaskResult{CandidateID: id, Status: "error", Reason: err.Error()})
		default:
			results = append(results, PageFetchTaskResult{CandidateID: id, TaskID: task.ID, Status: "created"})
		}
	}

	writeJSON(w, http.StatusAccepted, PageFetchResponse{Results: results})
}
