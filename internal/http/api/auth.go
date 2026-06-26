package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	defaultAuthListLimit = 50
	maxAuthListLimit     = 500
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

type AuthModel struct {
	ID          int16      `json:"id"`
	Name        string     `json:"name"`
	Provider    string     `json:"provider"`
	Type        string     `json:"type"`
	PublishDate *time.Time `json:"publish_date,omitempty"`
	URL         *string    `json:"url,omitempty"`
	Tag         *string    `json:"tag,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

type AuthSource struct {
	Abbr      string     `json:"abbr"`
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	BaseURL   string     `json:"base_url"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type AuthBatch struct {
	ID                   uuid.UUID  `json:"id"`
	SourceType           string     `json:"source_type"`
	TraceID              *string    `json:"trace_id,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	PublishedAt          *time.Time `json:"published_at,omitempty"`
	LastPublishAttemptAt *time.Time `json:"last_publish_attempt_at,omitempty"`
	PublishRetryCount    int        `json:"publish_retry_count"`
	PublishError         *string    `json:"publish_error,omitempty"`
	StalledAt            *time.Time `json:"stalled_at,omitempty"`
}

type AuthEntity struct {
	ID        int32     `json:"id"`
	Canonical string    `json:"canonical"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

type AuthSchedule struct {
	ID                     uuid.UUID       `json:"id"`
	Name                   string          `json:"name"`
	Enabled                bool            `json:"enabled"`
	ConfigPresent          bool            `json:"config_present"`
	ConfigHash             string          `json:"config_hash"`
	Kind                   string          `json:"kind"`
	SourceType             string          `json:"source_type"`
	SourceAbbr             string          `json:"source_abbr"`
	URL                    string          `json:"url"`
	Payload                json.RawMessage `json:"payload,omitempty"`
	Meta                   json.RawMessage `json:"meta,omitempty"`
	RunOnInsert            bool            `json:"run_on_insert"`
	NextFireAt             time.Time       `json:"next_fire_at"`
	LastFireAt             *time.Time      `json:"last_fire_at,omitempty"`
	LastMaterializedAt     *time.Time      `json:"last_materialized_at,omitempty"`
	LastMaterializedTaskID *uuid.UUID      `json:"last_materialized_task_id,omitempty"`
	LastError              *string         `json:"last_error,omitempty"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

type AuthEmbeddingRecord struct {
	ID        int64     `json:"id"`
	TargetID  uuid.UUID `json:"target_id"`
	ModelID   int16     `json:"model_id"`
	Category  string    `json:"category"`
	TraceID   string    `json:"trace_id"`
	CreatedAt time.Time `json:"created_at"`
}

type authListResponse[T any] struct {
	Items []T   `json:"items"`
	Limit int32 `json:"limit"`
	Next  int32 `json:"next"`
	Count int   `json:"count"`
}

type AuthEmbeddingListResponse struct {
	ModelName           string                `json:"model_name"`
	CandidateEmbeddings []AuthEmbeddingRecord `json:"candidate_embeddings"`
	ContentEmbeddings   []AuthEmbeddingRecord `json:"content_embeddings"`
	Limit               int32                 `json:"limit"`
	Next                int32                 `json:"next"`
	Count               int                   `json:"count"`
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

// ListAuthModels handles GET /api/v1/auth/models.
func (s *Server) ListAuthModels(w http.ResponseWriter, r *http.Request) {
	operator := s.operatorOrError(w)
	if operator == nil {
		return
	}
	params, ok := parseAuthListParams(w, r)
	if !ok {
		return
	}
	rows, err := operator.ListModels(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list auth models failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list models")
		return
	}
	items := make([]AuthModel, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAuthModel(row))
	}
	writeJSON(w, http.StatusOK, authListResponse[AuthModel]{Items: items, Limit: params.Limit, Next: params.Next, Count: len(items)})
}

// ListAuthSources handles GET /api/v1/auth/sources.
func (s *Server) ListAuthSources(w http.ResponseWriter, r *http.Request) {
	operator := s.operatorOrError(w)
	if operator == nil {
		return
	}
	params, ok := parseAuthListParams(w, r)
	if !ok {
		return
	}
	rows, err := operator.ListSources(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list auth sources failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list sources")
		return
	}
	items := make([]AuthSource, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAuthSource(row))
	}
	writeJSON(w, http.StatusOK, authListResponse[AuthSource]{Items: items, Limit: params.Limit, Next: params.Next, Count: len(items)})
}

// ListAuthBatches handles GET /api/v1/auth/batches.
func (s *Server) ListAuthBatches(w http.ResponseWriter, r *http.Request) {
	operator := s.operatorOrError(w)
	if operator == nil {
		return
	}
	params, ok := parseAuthListParams(w, r)
	if !ok {
		return
	}
	rows, err := operator.ListBatches(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list auth batches failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list batches")
		return
	}
	items := make([]AuthBatch, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAuthBatch(row))
	}
	writeJSON(w, http.StatusOK, authListResponse[AuthBatch]{Items: items, Limit: params.Limit, Next: params.Next, Count: len(items)})
}

// ListAuthEntities handles GET /api/v1/auth/entities.
func (s *Server) ListAuthEntities(w http.ResponseWriter, r *http.Request) {
	operator := s.operatorOrError(w)
	if operator == nil {
		return
	}
	params, ok := parseAuthListParams(w, r)
	if !ok {
		return
	}
	rows, err := operator.ListEntities(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list auth entities failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list entities")
		return
	}
	items := make([]AuthEntity, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAuthEntity(row))
	}
	writeJSON(w, http.StatusOK, authListResponse[AuthEntity]{Items: items, Limit: params.Limit, Next: params.Next, Count: len(items)})
}

// ListAuthSchedules handles GET /api/v1/auth/schedules.
func (s *Server) ListAuthSchedules(w http.ResponseWriter, r *http.Request) {
	operator := s.operatorOrError(w)
	if operator == nil {
		return
	}
	params, ok := parseAuthListParams(w, r)
	if !ok {
		return
	}
	rows, err := operator.ListSchedules(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list auth schedules failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list schedules")
		return
	}
	items := make([]AuthSchedule, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAuthSchedule(row))
	}
	writeJSON(w, http.StatusOK, authListResponse[AuthSchedule]{Items: items, Limit: params.Limit, Next: params.Next, Count: len(items)})
}

// ListAuthEmbeddings handles GET /api/v1/auth/embedding/{model_name}.
func (s *Server) ListAuthEmbeddings(w http.ResponseWriter, r *http.Request) {
	operator := s.operatorOrError(w)
	if operator == nil {
		return
	}
	modelName := strings.ToLower(strings.TrimSpace(r.PathValue("model_name")))
	if modelName != "embeddings_gemma_2025" && modelName != "gemma_2025" {
		writeError(w, http.StatusBadRequest, "unsupported embedding model")
		return
	}
	params, ok := parseAuthListParams(w, r)
	if !ok {
		return
	}
	candidateRows, err := operator.ListCandidateEmbeddingsGemma2025(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list auth candidate embeddings failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list candidate embeddings")
		return
	}
	contentRows, err := operator.ListContentEmbeddingsGemma2025(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list auth content embeddings failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list content embeddings")
		return
	}
	candidates := make([]AuthEmbeddingRecord, 0, len(candidateRows))
	for _, row := range candidateRows {
		candidates = append(candidates, toAuthEmbeddingRecord(row))
	}
	contents := make([]AuthEmbeddingRecord, 0, len(contentRows))
	for _, row := range contentRows {
		contents = append(contents, toAuthEmbeddingRecord(row))
	}
	writeJSON(w, http.StatusOK, AuthEmbeddingListResponse{
		ModelName:           "embeddings_gemma_2025",
		CandidateEmbeddings: candidates,
		ContentEmbeddings:   contents,
		Limit:               params.Limit,
		Next:                params.Next,
		Count:               len(candidates) + len(contents),
	})
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

func toAuthModel(model repo.Model) AuthModel {
	return AuthModel{
		ID:          model.ID,
		Name:        model.Name,
		Provider:    model.Provider,
		Type:        model.Type,
		PublishDate: model.PublishDate,
		URL:         model.URL,
		Tag:         model.Tag,
		CreatedAt:   model.CreatedAt,
		DeletedAt:   model.DeletedAt,
	}
}

func toAuthSource(source repo.Source) AuthSource {
	return AuthSource{
		Abbr:      source.Abbr,
		Name:      source.Name,
		Type:      source.Type,
		BaseURL:   source.BaseURL,
		CreatedAt: source.CreatedAt,
		DeletedAt: source.DeletedAt,
	}
}

func toAuthBatch(batch repo.Batch) AuthBatch {
	return AuthBatch{
		ID:                   batch.ID,
		SourceType:           batch.SourceType,
		TraceID:              batch.TraceID,
		CreatedAt:            batch.CreatedAt,
		UpdatedAt:            batch.UpdatedAt,
		CompletedAt:          batch.CompletedAt,
		PublishedAt:          batch.PublishedAt,
		LastPublishAttemptAt: batch.LastPublishAttemptAt,
		PublishRetryCount:    batch.PublishRetryCount,
		PublishError:         batch.PublishError,
		StalledAt:            batch.StalledAt,
	}
}

func toAuthEntity(entity repo.Entity) AuthEntity {
	return AuthEntity{
		ID:        entity.ID,
		Canonical: entity.Canonical,
		Type:      entity.Type,
		CreatedAt: entity.CreatedAt,
	}
}

func toAuthSchedule(schedule repo.Schedule) AuthSchedule {
	return AuthSchedule{
		ID:                     schedule.ID,
		Name:                   schedule.Name,
		Enabled:                schedule.Enabled,
		ConfigPresent:          schedule.ConfigPresent,
		ConfigHash:             schedule.ConfigHash,
		Kind:                   schedule.Kind,
		SourceType:             schedule.SourceType,
		SourceAbbr:             schedule.SourceAbbr,
		URL:                    schedule.URL,
		Payload:                rawJSON(schedule.Payload),
		Meta:                   rawJSON(schedule.Meta),
		RunOnInsert:            schedule.RunOnInsert,
		NextFireAt:             schedule.NextFireAt,
		LastFireAt:             schedule.LastFireAt,
		LastMaterializedAt:     schedule.LastMaterializedAt,
		LastMaterializedTaskID: schedule.LastMaterializedTaskID,
		LastError:              schedule.LastError,
		CreatedAt:              schedule.CreatedAt,
		UpdatedAt:              schedule.UpdatedAt,
	}
}

func toAuthEmbeddingRecord(record repo.EmbeddingRecord) AuthEmbeddingRecord {
	return AuthEmbeddingRecord{
		ID:        record.ID,
		TargetID:  record.TargetID,
		ModelID:   record.ModelID,
		Category:  record.Category,
		TraceID:   record.TraceID,
		CreatedAt: record.CreatedAt,
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

func (s *Server) operatorOrError(w http.ResponseWriter) repo.Operator {
	if s.Operator == nil {
		writeError(w, http.StatusInternalServerError, "operator repository unavailable")
		return nil
	}
	return s.Operator
}

func parseAuthListParams(w http.ResponseWriter, r *http.Request) (repo.ListOperatorParams, bool) {
	q := r.URL.Query()
	params := repo.ListOperatorParams{Limit: defaultAuthListLimit, Next: 1}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return repo.ListOperatorParams{}, false
		}
		if n > maxAuthListLimit {
			n = maxAuthListLimit
		}
		params.Limit = int32(n)
	}
	if raw := strings.TrimSpace(q.Get("next")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid next")
			return repo.ListOperatorParams{}, false
		}
		params.Next = int32(n)
	}
	return params, true
}
