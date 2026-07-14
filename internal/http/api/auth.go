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
	defaultAdminListLimit = 50
	maxAdminListLimit     = 500
)

// AdminTask is the operator JSON shape returned by admin task endpoints.
type AdminTask struct {
	ID          uuid.UUID       `json:"id"`
	BatchID     uuid.UUID       `json:"batch_id"`
	TraceID     string          `json:"trace_id"`
	Kind        string          `json:"kind"`
	SourceType  string          `json:"source_type"`
	SourceAbbr  string          `json:"source_abbr"`
	URL         string          `json:"url"`
	Payload     json.RawMessage `json:"payload,omitempty"       swaggertype:"object"`
	PayloadHash *string         `json:"payload_hash,omitempty"`
	Meta        json.RawMessage `json:"meta,omitempty"          swaggertype:"object"`
	Frequency   *time.Duration  `json:"frequency,omitempty"     swaggertype:"integer"`
	NextRunAt   time.Time       `json:"next_run_at"`
	ExpiresAt   *time.Time      `json:"expires_at,omitempty"`
	Status      repo.TaskStatus `json:"status"`
	RetryCount  int             `json:"retry_count"`
	LastRunAt   *time.Time      `json:"last_run_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type AdminListTasksResponse struct {
	Items   []AdminTask `json:"items"`
	BatchID uuid.UUID   `json:"batch_id"`
	Count   int         `json:"count"`
}

type AdminModel struct {
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

type AdminSource struct {
	Abbr      string     `json:"abbr"`
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	BaseURL   string     `json:"base_url"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type AdminBatch struct {
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

type AdminEntity struct {
	ID        int32     `json:"id"`
	Canonical string    `json:"canonical"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

type AdminSchedule struct {
	ID                     uuid.UUID       `json:"id"`
	Name                   string          `json:"name"`
	Enabled                bool            `json:"enabled"`
	ConfigPresent          bool            `json:"config_present"`
	ConfigHash             string          `json:"config_hash"`
	Kind                   string          `json:"kind"`
	SourceType             string          `json:"source_type"`
	SourceAbbr             string          `json:"source_abbr"`
	URL                    string          `json:"url"`
	Payload                json.RawMessage `json:"payload,omitempty"   swaggertype:"object"`
	Meta                   json.RawMessage `json:"meta,omitempty"      swaggertype:"object"`
	RunOnInsert            bool            `json:"run_on_insert"`
	NextFireAt             time.Time       `json:"next_fire_at"`
	LastFireAt             *time.Time      `json:"last_fire_at,omitempty"`
	LastMaterializedAt     *time.Time      `json:"last_materialized_at,omitempty"`
	LastMaterializedTaskID *uuid.UUID      `json:"last_materialized_task_id,omitempty"`
	LastError              *string         `json:"last_error,omitempty"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

type AdminEmbeddingRecord struct {
	ID        int64     `json:"id"`
	TargetID  uuid.UUID `json:"target_id"`
	ModelID   int16     `json:"model_id"`
	Category  string    `json:"category"`
	TraceID   string    `json:"trace_id"`
	CreatedAt time.Time `json:"created_at"`
}

type adminListResponse[T any] struct {
	Items []T   `json:"items"`
	Limit int32 `json:"limit"`
	Next  int32 `json:"next"`
	Count int   `json:"count"`
}

// AdminListModelsResponse is the documented response shape for ListAdminModels.
type AdminListModelsResponse struct {
	Items []AdminModel `json:"items"`
	Limit int32        `json:"limit"`
	Next  int32        `json:"next"`
	Count int          `json:"count"`
}

// AdminListSourcesResponse is the documented response shape for ListAdminSources.
type AdminListSourcesResponse struct {
	Items []AdminSource `json:"items"`
	Limit int32         `json:"limit"`
	Next  int32         `json:"next"`
	Count int           `json:"count"`
}

// AdminListBatchesResponse is the documented response shape for ListAdminBatches.
type AdminListBatchesResponse struct {
	Items []AdminBatch `json:"items"`
	Limit int32        `json:"limit"`
	Next  int32        `json:"next"`
	Count int          `json:"count"`
}

// AdminListEntitiesResponse is the documented response shape for ListAdminEntities.
type AdminListEntitiesResponse struct {
	Items []AdminEntity `json:"items"`
	Limit int32         `json:"limit"`
	Next  int32         `json:"next"`
	Count int           `json:"count"`
}

// AdminListSchedulesResponse is the documented response shape for ListAdminSchedules.
type AdminListSchedulesResponse struct {
	Items []AdminSchedule `json:"items"`
	Limit int32           `json:"limit"`
	Next  int32           `json:"next"`
	Count int             `json:"count"`
}

type AdminEmbeddingListResponse struct {
	ModelName           string                 `json:"model_name"`
	CandidateEmbeddings []AdminEmbeddingRecord `json:"candidate_embeddings"`
	ContentEmbeddings   []AdminEmbeddingRecord `json:"content_embeddings"`
	Limit               int32                  `json:"limit"`
	Next                int32                  `json:"next"`
	Count               int                    `json:"count"`
}

// AdminCandidate is the operator JSON shape returned by admin candidate endpoints.
type AdminCandidate struct {
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
	Metadata        json.RawMessage `json:"metadata,omitempty" swaggertype:"object"`
	CreatedAt       time.Time       `json:"created_at"`
}

// ListAdminModels handles GET /api/v1/admin/models.
//
// @Summary   List operator models
// @Tags      admin
// @Produce   json
// @Param     limit query int false "Page size (default 50, max 500)"
// @Param     next  query int false "Cursor for next page (default 1)"
// @Success   200 {object} AdminListModelsResponse
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/models [get]
func (s *Server) ListAdminModels(w http.ResponseWriter, r *http.Request) {
	operator := s.operatorOrError(w)
	if operator == nil {
		return
	}
	params, ok := parseAdminListParams(w, r)
	if !ok {
		return
	}
	rows, err := operator.ListModels(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list admin models failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list models")
		return
	}
	items := make([]AdminModel, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAdminModel(row))
	}
	writeJSON(w, http.StatusOK, adminListResponse[AdminModel]{Items: items, Limit: params.Limit, Next: params.Next, Count: len(items)})
}

// ListAdminSources handles GET /api/v1/admin/sources.
//
// @Summary   List operator sources
// @Tags      admin
// @Produce   json
// @Param     limit query int false "Page size (default 50, max 500)"
// @Param     next  query int false "Cursor for next page (default 1)"
// @Success   200 {object} AdminListSourcesResponse
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/sources [get]
func (s *Server) ListAdminSources(w http.ResponseWriter, r *http.Request) {
	operator := s.operatorOrError(w)
	if operator == nil {
		return
	}
	params, ok := parseAdminListParams(w, r)
	if !ok {
		return
	}
	rows, err := operator.ListSources(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list admin sources failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list sources")
		return
	}
	items := make([]AdminSource, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAdminSource(row))
	}
	writeJSON(w, http.StatusOK, adminListResponse[AdminSource]{Items: items, Limit: params.Limit, Next: params.Next, Count: len(items)})
}

// ListAdminBatches handles GET /api/v1/admin/batches.
//
// @Summary   List operator batches
// @Tags      admin
// @Produce   json
// @Param     limit query int false "Page size (default 50, max 500)"
// @Param     next  query int false "Cursor for next page (default 1)"
// @Success   200 {object} AdminListBatchesResponse
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/batches [get]
func (s *Server) ListAdminBatches(w http.ResponseWriter, r *http.Request) {
	operator := s.operatorOrError(w)
	if operator == nil {
		return
	}
	params, ok := parseAdminListParams(w, r)
	if !ok {
		return
	}
	rows, err := operator.ListBatches(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list admin batches failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list batches")
		return
	}
	items := make([]AdminBatch, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAdminBatch(row))
	}
	writeJSON(w, http.StatusOK, adminListResponse[AdminBatch]{Items: items, Limit: params.Limit, Next: params.Next, Count: len(items)})
}

// ListAdminEntities handles GET /api/v1/admin/entities.
//
// @Summary   List operator entities
// @Tags      admin
// @Produce   json
// @Param     limit query int false "Page size (default 50, max 500)"
// @Param     next  query int false "Cursor for next page (default 1)"
// @Success   200 {object} AdminListEntitiesResponse
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/entities [get]
func (s *Server) ListAdminEntities(w http.ResponseWriter, r *http.Request) {
	operator := s.operatorOrError(w)
	if operator == nil {
		return
	}
	params, ok := parseAdminListParams(w, r)
	if !ok {
		return
	}
	rows, err := operator.ListEntities(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list admin entities failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list entities")
		return
	}
	items := make([]AdminEntity, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAdminEntity(row))
	}
	writeJSON(w, http.StatusOK, adminListResponse[AdminEntity]{Items: items, Limit: params.Limit, Next: params.Next, Count: len(items)})
}

// ListAdminSchedules handles GET /api/v1/admin/schedules.
//
// @Summary   List operator schedules
// @Tags      admin
// @Produce   json
// @Param     limit query int false "Page size (default 50, max 500)"
// @Param     next  query int false "Cursor for next page (default 1)"
// @Success   200 {object} AdminListSchedulesResponse
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/schedules [get]
func (s *Server) ListAdminSchedules(w http.ResponseWriter, r *http.Request) {
	operator := s.operatorOrError(w)
	if operator == nil {
		return
	}
	params, ok := parseAdminListParams(w, r)
	if !ok {
		return
	}
	rows, err := operator.ListSchedules(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list admin schedules failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list schedules")
		return
	}
	items := make([]AdminSchedule, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAdminSchedule(row))
	}
	writeJSON(w, http.StatusOK, adminListResponse[AdminSchedule]{Items: items, Limit: params.Limit, Next: params.Next, Count: len(items)})
}

// ListAdminEmbeddings handles GET /api/v1/admin/embedding/{model_name}.
//
// @Summary   List operator embeddings by model
// @Tags      admin
// @Produce   json
// @Param     model_name path  string true  "Embedding model name"
// @Param     limit      query int    false "Page size (default 50, max 500)"
// @Param     next       query int    false "Cursor for next page (default 1)"
// @Success   200 {object} AdminEmbeddingListResponse
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/embedding/{model_name} [get]
func (s *Server) ListAdminEmbeddings(w http.ResponseWriter, r *http.Request) {
	operator := s.operatorOrError(w)
	if operator == nil {
		return
	}
	modelName := strings.ToLower(strings.TrimSpace(r.PathValue("model_name")))
	if modelName != "embeddings_gemma_2025" && modelName != "gemma_2025" {
		writeError(w, http.StatusBadRequest, "unsupported embedding model")
		return
	}
	params, ok := parseAdminListParams(w, r)
	if !ok {
		return
	}
	candidateRows, err := operator.ListCandidateEmbeddingsGemma2025(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list admin candidate embeddings failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list candidate embeddings")
		return
	}
	contentRows, err := operator.ListContentEmbeddingsGemma2025(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list admin content embeddings failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list content embeddings")
		return
	}
	candidates := make([]AdminEmbeddingRecord, 0, len(candidateRows))
	for _, row := range candidateRows {
		candidates = append(candidates, toAdminEmbeddingRecord(row))
	}
	contents := make([]AdminEmbeddingRecord, 0, len(contentRows))
	for _, row := range contentRows {
		contents = append(contents, toAdminEmbeddingRecord(row))
	}
	writeJSON(w, http.StatusOK, AdminEmbeddingListResponse{
		ModelName:           "embeddings_gemma_2025",
		CandidateEmbeddings: candidates,
		ContentEmbeddings:   contents,
		Limit:               params.Limit,
		Next:                params.Next,
		Count:               len(candidates) + len(contents),
	})
}

// GetAdminTask handles GET /api/v1/admin/tasks/{id}.
//
// @Summary   Get operator task
// @Tags      admin
// @Produce   json
// @Param     id path string true "Task UUID"
// @Success   200 {object} AdminTask
// @Failure   400 {object} ErrorResponse
// @Failure   404 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/tasks/{id} [get]
func (s *Server) GetAdminTask(w http.ResponseWriter, r *http.Request) {
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
		s.Logger.ErrorContext(r.Context(), "get admin task failed", slog.String("task_id", taskID.String()), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}
	writeJSON(w, http.StatusOK, toAdminTask(task))
}

// RetryAdminTask handles POST /api/v1/admin/tasks/{id}/retry.
//
// @Summary   Retry failed operator task
// @Tags      admin
// @Produce   json
// @Param     id path string true "Task UUID"
// @Success   200 {object} AdminTask
// @Failure   400 {object} ErrorResponse
// @Failure   404 {object} ErrorResponse
// @Failure   409 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/tasks/{id}/retry [post]
func (s *Server) RetryAdminTask(w http.ResponseWriter, r *http.Request) {
	taskID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}

	task, err := s.Tasks.RetryFailedTask(r.Context(), taskID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, http.StatusNotFound, "task not found")
		case errors.Is(err, repo.ErrTaskNotFailed):
			writeError(w, http.StatusConflict, "task is not failed")
		default:
			s.Logger.ErrorContext(r.Context(), "retry admin task failed", slog.String("task_id", taskID.String()), slog.Any("error", err))
			writeError(w, http.StatusInternalServerError, "failed to retry task")
		}
		return
	}
	writeJSON(w, http.StatusOK, toAdminTask(task))
}

// ListAdminTasks handles GET /api/v1/admin/tasks?batch_id=...
//
// @Summary   List operator tasks by batch
// @Tags      admin
// @Produce   json
// @Param     batch_id query string true "Batch UUID"
// @Success   200 {object} AdminListTasksResponse
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/tasks [get]
func (s *Server) ListAdminTasks(w http.ResponseWriter, r *http.Request) {
	batchID, err := uuid.Parse(r.URL.Query().Get("batch_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid batch_id")
		return
	}

	rows, err := s.Tasks.ListTasksByBatchID(r.Context(), batchID)
	if err != nil {
		s.Logger.ErrorContext(
			r.Context(),
			"list admin tasks failed",
			slog.String("batch_id", batchID.String()),
			slog.Any("error", err),
		)

		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}
	items := make([]AdminTask, 0, len(rows))
	for _, task := range rows {
		items = append(items, toAdminTask(task))
	}
	writeJSON(
		w, http.StatusOK,
		AdminListTasksResponse{Items: items, BatchID: batchID, Count: len(items)},
	)
}

// GetAdminCandidate handles GET /api/v1/admin/candidates/{id}.
//
// @Summary   Get operator candidate
// @Tags      admin
// @Produce   json
// @Param     id path string true "Candidate UUID"
// @Success   200 {object} AdminCandidate
// @Failure   400 {object} ErrorResponse
// @Failure   404 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /admin/candidates/{id} [get]
func (s *Server) GetAdminCandidate(w http.ResponseWriter, r *http.Request) {
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
		s.Logger.ErrorContext(
			r.Context(), "get admin candidate failed",
			slog.String("candidate_id", candidateID.String()),
			slog.Any("error", err),
		)
		writeError(w, http.StatusInternalServerError, "failed to load candidate")
		return
	}
	writeJSON(w, http.StatusOK, toAdminCandidate(candidate))
}

func toAdminTask(task repo.Task) AdminTask {
	return AdminTask{
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

func toAdminModel(model repo.Model) AdminModel {
	return AdminModel{
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

func toAdminSource(source repo.Source) AdminSource {
	return AdminSource{
		Abbr:      source.Abbr,
		Name:      source.Name,
		Type:      source.Type,
		BaseURL:   source.BaseURL,
		CreatedAt: source.CreatedAt,
		DeletedAt: source.DeletedAt,
	}
}

func toAdminBatch(batch repo.Batch) AdminBatch {
	return AdminBatch{
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

func toAdminEntity(entity repo.Entity) AdminEntity {
	return AdminEntity{
		ID:        entity.ID,
		Canonical: entity.Canonical,
		Type:      entity.Type,
		CreatedAt: entity.CreatedAt,
	}
}

func toAdminSchedule(schedule repo.Schedule) AdminSchedule {
	return AdminSchedule{
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

func toAdminEmbeddingRecord(record repo.EmbeddingRecord) AdminEmbeddingRecord {
	return AdminEmbeddingRecord{
		ID:        record.ID,
		TargetID:  record.TargetID,
		ModelID:   record.ModelID,
		Category:  record.Category,
		TraceID:   record.TraceID,
		CreatedAt: record.CreatedAt,
	}
}

func toAdminCandidate(candidate repo.Candidate) AdminCandidate {
	return AdminCandidate{
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

func parseAdminListParams(w http.ResponseWriter, r *http.Request) (repo.ListOperatorParams, bool) {
	q := r.URL.Query()
	params := repo.ListOperatorParams{Limit: defaultAdminListLimit, Next: 1}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return repo.ListOperatorParams{}, false
		}
		if n > maxAdminListLimit {
			n = maxAdminListLimit
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
