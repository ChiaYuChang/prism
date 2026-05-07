package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxPageFetchBatch = 100

// Per-item status values returned by POST /page_fetch. Three values only:
// `created` collapses fresh-insert and shared-active-task to avoid leaking
// cross-user / cross-request activity. `task_id` is never exposed.
const (
	PageFetchStatusCreated         = "created"
	PageFetchStatusAlreadyComplete = "already_complete"
	PageFetchStatusNotFound        = "not_found"
)

type PageFetchRequest struct {
	CandidateIDs []uuid.UUID `json:"candidate_ids"`
}

// PageFetchItem is the per-candidate response entry.
type PageFetchItem struct {
	CandidateID uuid.UUID `json:"candidate_id"`
	Status      string    `json:"status"`
}

// PageFetchResponse is returned by POST /api/v1/page_fetch.
//
// Items preserve input candidate_id order. See docs/plan/spec.md §6 for
// the rationale on the three-status collapse.
type PageFetchResponse struct {
	FetchID uuid.UUID       `json:"fetch_id"`
	Items   []PageFetchItem `json:"items"`
}

// PageFetch handles POST /api/v1/page_fetch.
//
// Creates a user_fetch_request grouping the supplied candidates, then for
// each candidate either creates a PAGE_FETCH task, attaches to an
// existing active task with the same URL, or records an
// `already_complete` snapshot when contents are already present.
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

	fetch, err := s.UserFetches.Create(ctx, repo.CreateUserFetchParams{UserID: nil})
	if err != nil {
		s.Logger.ErrorContext(ctx, "create user fetch failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to create fetch")
		return
	}

	items := make([]PageFetchItem, 0, len(req.CandidateIDs))
	for _, id := range req.CandidateIDs {
		c, ok := byID[id]
		if !ok {
			items = append(items, PageFetchItem{CandidateID: id, Status: PageFetchStatusNotFound})
			continue
		}

		status, err := s.recordPageFetchItem(ctx, fetch.ID, c)
		if err != nil {
			s.Logger.ErrorContext(ctx, "record page fetch item failed",
				slog.String("candidate_id", id.String()),
				slog.String("fetch_id", fetch.ID.String()),
				slog.Any("error", err))
			writeError(w, http.StatusInternalServerError, "failed to record fetch item")
			return
		}
		items = append(items, PageFetchItem{CandidateID: id, Status: status})
	}

	writeJSON(w, http.StatusAccepted, PageFetchResponse{FetchID: fetch.ID, Items: items})
}

// recordPageFetchItem persists one fetch_items row and returns its public
// status.
//
// Flow per docs/plan/spec.md §6:
//
//  1. CreateTask success (inserted) → item references the new task; status
//     `created`.
//  2. CreateTask returns ErrTaskAlreadyActive with the existing active task
//     populated → item references the shared task; status `created`
//     (collapsed for privacy).
//  3. Existing-task path with PAGE_FETCH already drained → contents
//     already present → item snapshot `ALREADY_COMPLETE`, task_id NULL;
//     status `already_complete`. Detected by querying contents when the
//     recovered task is no longer active.
//
// The "active task drained without contents row" window is non-existent
// by design (collector ordering CreateContent → CompleteTask), so any
// remaining miss is an invariant violation and surfaces as 500.
func (s *Server) recordPageFetchItem(ctx context.Context, fetchID uuid.UUID, c repo.Candidate) (string, error) {
	meta, err := json.Marshal(map[string]any{"candidate_id": c.ID.String()})
	if err != nil {
		return "", err
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
	case err == nil:
		taskID := task.ID
		if _, err := s.UserFetches.CreateItem(ctx, repo.CreateUserFetchItemParams{
			FetchID:     fetchID,
			CandidateID: c.ID,
			TaskID:      &taskID,
		}); err != nil {
			return "", err
		}
		return PageFetchStatusCreated, nil

	case errors.Is(err, repo.ErrTaskAlreadyActive):
		if task.ID != uuid.Nil {
			taskID := task.ID
			if _, err := s.UserFetches.CreateItem(ctx, repo.CreateUserFetchItemParams{
				FetchID:     fetchID,
				CandidateID: c.ID,
				TaskID:      &taskID,
			}); err != nil {
				return "", err
			}
			return PageFetchStatusCreated, nil
		}

		// Race: conflict at insert but the colliding task was no longer
		// PENDING/RUNNING by recovery-SELECT time. Collector ordering
		// (CreateContent → CompleteTask) means the contents row must
		// exist by now.
		content, contentErr := s.Pipeline.GetContentByURL(ctx, c.URL)
		if contentErr != nil {
			if errors.Is(contentErr, pgx.ErrNoRows) {
				return "", errors.New("page_fetch race: active task drained without contents row (design invariant)")
			}
			return "", contentErr
		}
		_ = content
		snapshot := repo.UserFetchItemSnapshotAlreadyComplete
		if _, err := s.UserFetches.CreateItem(ctx, repo.CreateUserFetchItemParams{
			FetchID:        fetchID,
			CandidateID:    c.ID,
			SnapshotStatus: &snapshot,
		}); err != nil {
			return "", err
		}
		return PageFetchStatusAlreadyComplete, nil

	default:
		return "", err
	}
}
