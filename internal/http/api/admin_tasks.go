package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

type adminCreateTaskRequest struct {
	BatchID    uuid.UUID       `json:"batch_id"`
	Kind       string          `json:"kind"`
	SourceType string          `json:"source_type"`
	SourceAbbr string          `json:"source_abbr"`
	URL        string          `json:"url"`
	Payload    json.RawMessage `json:"payload,omitempty" swaggertype:"object"`
	Meta       json.RawMessage `json:"meta,omitempty" swaggertype:"object"`
	TraceID    string          `json:"trace_id"`
	NextRunAt  *time.Time      `json:"next_run_at,omitempty"`
	ExpiresAt  *time.Time      `json:"expires_at,omitempty"`
}

// CreateAdminTask handles POST /api/v1/admin/tasks.
//
// @Summary   Create controlled operator task
// @Tags      admin
// @Accept    json
// @Produce   json
// @Param     request body adminCreateTaskRequest true "Task request"
// @Success   201 {object} AdminTask
// @Failure   400 {object} ErrorResponse
// @Failure   409 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/tasks [post]
//
// This is intentionally a narrow operator injection point for controlled
// replay and smoke tests. Normal production task creation remains owned by
// schedules, discovery, and planner flows.
func (s *Server) CreateAdminTask(w http.ResponseWriter, r *http.Request) {
	var req adminCreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.BatchID == uuid.Nil || strings.TrimSpace(req.Kind) == "" ||
		strings.TrimSpace(req.SourceType) == "" || strings.TrimSpace(req.SourceAbbr) == "" ||
		strings.TrimSpace(req.TraceID) == "" {
		writeError(w, http.StatusBadRequest, "batch_id, kind, source_type, source_abbr, and trace_id are required")
		return
	}
	if _, err := url.ParseRequestURI(strings.TrimSpace(req.URL)); err != nil || !strings.Contains(req.URL, "://") {
		writeError(w, http.StatusBadRequest, "url must be an absolute URL")
		return
	}
	if len(req.Payload) > 0 && !json.Valid(req.Payload) {
		writeError(w, http.StatusBadRequest, "payload must be valid JSON")
		return
	}
	if len(req.Meta) > 0 && !json.Valid(req.Meta) {
		writeError(w, http.StatusBadRequest, "meta must be valid JSON")
		return
	}

	task, err := s.Tasks.CreateTask(r.Context(), repo.CreateTaskParams{
		BatchID:    req.BatchID,
		Kind:       strings.TrimSpace(req.Kind),
		SourceType: strings.TrimSpace(req.SourceType),
		SourceAbbr: strings.TrimSpace(req.SourceAbbr),
		URL:        strings.TrimSpace(req.URL),
		Payload:    req.Payload,
		Meta:       req.Meta,
		TraceID:    strings.TrimSpace(req.TraceID),
		NextRunAt:  req.NextRunAt,
		ExpiresAt:  req.ExpiresAt,
	})
	if err != nil {
		if errors.Is(err, repo.ErrTaskAlreadyActive) {
			writeError(w, http.StatusConflict, "task is already active")
			return
		}
		s.Logger.ErrorContext(r.Context(), "create admin task failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create task")
		return
	}
	writeJSON(w, http.StatusCreated, toAdminTask(task))
}
