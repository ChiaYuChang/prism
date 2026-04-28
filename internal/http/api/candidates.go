package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

const (
	defaultCandidateLimit = 50
	maxCandidateLimit     = 200
)

// Candidate is the JSON shape returned by /candidates endpoints.
type Candidate struct {
	ID              uuid.UUID  `json:"id"`
	BatchID         uuid.UUID  `json:"batch_id"`
	SourceAbbr      string     `json:"source_abbr"`
	Title           string     `json:"title"`
	URL             string     `json:"url"`
	Description     *string    `json:"description,omitempty"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	DiscoveredAt    time.Time  `json:"discovered_at"`
	IngestionMethod string     `json:"ingestion_method"`
	TraceID         string     `json:"trace_id"`
}

type ListCandidatesResponse struct {
	Items  []Candidate `json:"items"`
	Limit  int32       `json:"limit"`
	Offset int32       `json:"offset"`
	Count  int         `json:"count"`
}

// ListCandidates handles GET /api/v1/candidates.
//
// @Summary   List candidate article briefs
// @Tags      candidates
// @Produce   json
// @Param     q           query string false "Keyword matched against title/description (ILIKE)"
// @Param     source_abbr query string false "Filter by source abbreviation (e.g. dpp, tpp, yahoo)"
// @Param     since       query string false "Lower bound on published_at/discovered_at (RFC3339)"
// @Param     until       query string false "Upper bound on published_at/discovered_at (RFC3339)"
// @Param     limit       query int    false "Page size (default 50, max 200)"
// @Param     offset      query int    false "Pagination offset (default 0)"
// @Success   200 {object} ListCandidatesResponse
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /candidates [get]
func (s *Server) ListCandidates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	params := repo.ListCandidatesParams{
		Limit:  defaultCandidateLimit,
		Offset: 0,
	}
	if v := strings.TrimSpace(q.Get("q")); v != "" {
		params.Query = &v
	}
	if v := strings.TrimSpace(q.Get("source_abbr")); v != "" {
		params.SourceAbbr = &v
	}
	if v := strings.TrimSpace(q.Get("since")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since: expected RFC3339")
			return
		}
		params.Since = &t
	}
	if v := strings.TrimSpace(q.Get("until")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid until: expected RFC3339")
			return
		}
		params.Until = &t
	}
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if n > maxCandidateLimit {
			n = maxCandidateLimit
		}
		params.Limit = int32(n)
	}
	if v := strings.TrimSpace(q.Get("offset")); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid offset")
			return
		}
		params.Offset = int32(n)
	}

	rows, err := s.Scout.ListCandidates(r.Context(), params)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "list candidates failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to list candidates")
		return
	}

	items := make([]Candidate, 0, len(rows))
	for _, c := range rows {
		items = append(items, toCandidate(c))
	}
	writeJSON(w, http.StatusOK, ListCandidatesResponse{
		Items:  items,
		Limit:  params.Limit,
		Offset: params.Offset,
		Count:  len(items),
	})
}

func toCandidate(c repo.Candidate) Candidate {
	return Candidate{
		ID:              c.ID,
		BatchID:         c.BatchID,
		SourceAbbr:      c.SourceAbbr,
		Title:           c.Title,
		URL:             c.URL,
		Description:     c.Description,
		PublishedAt:     c.PublishedAt,
		DiscoveredAt:    c.DiscoveredAt,
		IngestionMethod: c.IngestionMethod,
		TraceID:         c.TraceID,
	}
}
