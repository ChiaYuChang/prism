package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ChiaYuChang/prism/internal/repo"
)

type modelRequest struct {
	Name        string  `json:"name"`
	Provider    string  `json:"provider"`
	Type        string  `json:"type"`
	PublishDate *string `json:"publish_date,omitempty"`
	URL         *string `json:"url,omitempty"`
	Tag         *string `json:"tag,omitempty"`
}

// CreateAdminModel handles POST /api/v1/admin/models.
func (s *Server) CreateAdminModel(w http.ResponseWriter, r *http.Request) {
	var req modelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	provider := strings.TrimSpace(req.Provider)
	typ := strings.ToUpper(strings.TrimSpace(req.Type))
	if name == "" || provider == "" || (typ != "EXTRACTOR" && typ != "EMBEDDER" && typ != "ANALYZER") {
		writeError(w, http.StatusBadRequest, "name, provider, and type (EXTRACTOR, EMBEDDER, or ANALYZER) are required")
		return
	}
	if s.Operator == nil {
		writeError(w, http.StatusServiceUnavailable, "model management is disabled")
		return
	}
	model, err := s.Operator.CreateModel(r.Context(), repo.CreateModelParams{
		Name:     name,
		Provider: provider,
		Type:     typ,
		URL:      req.URL,
		Tag:      req.Tag,
	})
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "create admin model failed", "model", name, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create model")
		return
	}
	writeJSON(w, http.StatusCreated, toAdminModel(model))
}
