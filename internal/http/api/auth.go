package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AuthTask is the operator JSON shape returned by authenticated task endpoints.
type AuthTask struct {
	ID          uuid.UUID       `json:"id"`
	BatchID     uuid.UUID       `json:"batch_id"`
	TraceID     string          `json:"trace_id"`
	Kind        string          `json:"kind"`
	SourceType  string          `json:"source_type"`
	SourceAbbr  string          `json:"source_abbr"`
	URL         string          `json:"url"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	PayloadHash *string         `json:"payload_hash,omitempty"`
	Meta        json.RawMessage `json:"meta,omitempty"`
	Frequency   *time.Duration  `json:"frequency,omitempty"`
	NextRunAt   time.Time       `json:"next_run_at"`
	ExpiresAt   *time.Time      `json:"expires_at,omitempty"`
	Status      repo.TaskStatus `json:"status"`
	RetryCount  int             `json:"retry_count"`
	LastRunAt   *time.Time      `json:"last_run_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type AuthListTasksResponse struct {
	Items   []AuthTask `json:"items"`
	BatchID uuid.UUID  `json:"batch_id"`
	Count   int        `json:"count"`
}

// AuthCandidate is the operator JSON shape returned by authenticated candidate endpoints.
type AuthCandidate struct {
	ID              uuid.UUID       `json:"id"`
	BatchID         uuid.UUID       `json:"batch_id"`
	Fingerprint     string          `json:"fingerprint"`
	SourceAbbr      string          `json:"source_abbr"`
	Title           string          `json:"title"`
	URL             string          `json:"url"`
	Description     *string         `json:"description,omitempty"`
	PublishedAt     *time.Time      `json:"published_at,omitempty"`
	DiscoveredAt    time.Time       `json:"discovered_at"`
	TraceID         string          `json:"trace_id"`
	IngestionMethod string          `json:"ingestion_method"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

// GetAuthTask handles GET /api/v1/auth/tasks/{id}.
func (s *Server) GetAuthTask(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}

	task, err := s.Tasks.GetTaskByID(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		s.Logger.ErrorContext(r.Context(), "get auth task failed", slog.String("task_id", taskID.String()), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}
	writeJSON(w, http.StatusOK, toAuthTask(task))
}

// ListAuthTasks handles GET /api/v1/auth/tasks?batch_id=...
func (s *Server) ListAuthTasks(w http.ResponseWriter, r *http.Request) {
	batchID, err := uuid.Parse(r.URL.Query().Get("batch_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid batch_id")
		return
	}

	rows, err := s.Tasks.ListTasksByBatchID(r.Context(), batchID)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list auth tasks failed", slog.String("batch_id", batchID.String()), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}
	items := make([]AuthTask, 0, len(rows))
	for _, task := range rows {
		items = append(items, toAuthTask(task))
	}
	writeJSON(w, http.StatusOK, AuthListTasksResponse{Items: items, BatchID: batchID, Count: len(items)})
}

// GetAuthCandidate handles GET /api/v1/auth/candidates/{id}.
func (s *Server) GetAuthCandidate(w http.ResponseWriter, r *http.Request) {
	candidateID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid candidate id")
		return
	}

	candidate, err := s.Scout.GetCandidateByID(r.Context(), candidateID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "candidate not found")
			return
		}
		s.Logger.ErrorContext(r.Context(), "get auth candidate failed", slog.String("candidate_id", candidateID.String()), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to load candidate")
		return
	}
	writeJSON(w, http.StatusOK, toAuthCandidate(candidate))
}

func toAuthTask(task repo.Task) AuthTask {
	return AuthTask{
		ID:          task.ID,
		BatchID:     task.BatchID,
		TraceID:     task.TraceID,
		Kind:        task.Kind,
		SourceType:  task.SourceType,
		SourceAbbr:  task.SourceAbbr,
		URL:         task.URL,
		Payload:     rawJSON(task.Payload),
		PayloadHash: task.PayloadHash,
		Meta:        rawJSON(task.Meta),
		Frequency:   task.Frequency,
		NextRunAt:   task.NextRunAt,
		ExpiresAt:   task.ExpiresAt,
		Status:      task.Status,
		RetryCount:  task.RetryCount,
		LastRunAt:   task.LastRunAt,
		CreatedAt:   task.CreatedAt,
		UpdatedAt:   task.UpdatedAt,
	}
}

func toAuthCandidate(candidate repo.Candidate) AuthCandidate {
	return AuthCandidate{
		ID:              candidate.ID,
		BatchID:         candidate.BatchID,
		Fingerprint:     candidate.Fingerprint,
		SourceAbbr:      candidate.SourceAbbr,
		Title:           candidate.Title,
		URL:             candidate.URL,
		Description:     candidate.Description,
		PublishedAt:     candidate.PublishedAt,
		DiscoveredAt:    candidate.DiscoveredAt,
		TraceID:         candidate.TraceID,
		IngestionMethod: candidate.IngestionMethod,
		Metadata:        rawJSON(candidate.Metadata),
		CreatedAt:       candidate.CreatedAt,
	}
}

func rawJSON(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}
