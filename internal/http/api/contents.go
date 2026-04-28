package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Content is the JSON shape returned by /contents endpoints.
type Content struct {
	ID          uuid.UUID `json:"id"`
	BatchID     uuid.UUID `json:"batch_id"`
	Type        string    `json:"type"`
	SourceAbbr  string    `json:"source_abbr"`
	CandidateID uuid.UUID `json:"candidate_id"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	Author      *string   `json:"author,omitempty"`
	PublishedAt time.Time `json:"published_at"`
	FetchedAt   time.Time `json:"fetched_at"`
	TraceID     string    `json:"trace_id"`
}

// GetContent handles GET /api/v1/contents/{candidate_id}.
//
// Returns 404 while the content is still pending (not yet fetched by the
// collector). Clients should poll periodically after POST /page_fetch.
//
// @Summary   Get fetched content for a candidate
// @Tags      contents
// @Produce   json
// @Param     candidate_id path string true "Candidate UUID"
// @Success   200 {object} Content
// @Failure   400 {object} ErrorResponse
// @Failure   404 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /contents/{candidate_id} [get]
func (s *Server) GetContent(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("candidate_id")
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid candidate_id")
		return
	}

	ctx := r.Context()
	content, err := s.Pipeline.GetContentByCandidateID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "content not yet available")
			return
		}
		s.Logger.ErrorContext(ctx, "get content failed",
			slog.String("candidate_id", id.String()), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to load content")
		return
	}

	writeJSON(w, http.StatusOK, Content{
		ID:          content.ID,
		BatchID:     content.BatchID,
		Type:        content.Type,
		SourceAbbr:  content.SourceAbbr,
		CandidateID: content.CandidateID,
		URL:         content.URL,
		Title:       content.Title,
		Content:     content.Content,
		Author:      content.Author,
		PublishedAt: content.PublishedAt,
		FetchedAt:   content.FetchedAt,
		TraceID:     content.TraceID,
	})
}
