package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/ChiaYuChang/prism/internal/repo"
)

var sourceAbbrPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,15}$`)

type sourceRequest struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	BaseURL string `json:"base_url"`
}

// CreateAdminSource handles POST /api/v1/admin/sources?abbr=npp.
func (s *Server) CreateAdminSource(w http.ResponseWriter, r *http.Request) {
	var req sourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	abbr := strings.TrimSpace(r.URL.Query().Get("abbr"))
	if err := validateSource(abbr, req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Sources == nil {
		writeError(w, http.StatusServiceUnavailable, "source management is disabled")
		return
	}
	source, err := s.Sources.Create(r.Context(), sourceParams(abbr, req))
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "create source failed", "source_abbr", abbr, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create source")
		return
	}
	writeJSON(w, http.StatusCreated, toAdminSource(source))
}

// UpdateAdminSource handles PUT /api/v1/admin/sources/{abbr}.
func (s *Server) UpdateAdminSource(w http.ResponseWriter, r *http.Request) {
	abbr := strings.TrimSpace(r.PathValue("abbr"))
	var req sourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := validateSource(abbr, req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Sources == nil {
		writeError(w, http.StatusServiceUnavailable, "source management is disabled")
		return
	}
	source, err := s.Sources.Update(r.Context(), sourceParams(abbr, req))
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "update source failed", "source_abbr", abbr, "error", err)
		writeError(w, http.StatusNotFound, "source not found")
		return
	}
	writeJSON(w, http.StatusOK, toAdminSource(source))
}

// DeleteAdminSource handles DELETE /api/v1/admin/sources/{abbr}.
func (s *Server) DeleteAdminSource(w http.ResponseWriter, r *http.Request) {
	source, ok := s.mutateSource(w, r, false)
	if ok {
		writeJSON(w, http.StatusOK, toAdminSource(source))
	}
}

// RestoreAdminSource handles POST /api/v1/admin/sources/{abbr}/restore.
func (s *Server) RestoreAdminSource(w http.ResponseWriter, r *http.Request) {
	source, ok := s.mutateSource(w, r, true)
	if ok {
		writeJSON(w, http.StatusOK, toAdminSource(source))
	}
}

func (s *Server) mutateSource(w http.ResponseWriter, r *http.Request, restore bool) (repo.Source, bool) {
	if s.Sources == nil {
		writeError(w, http.StatusServiceUnavailable, "source management is disabled")
		return repo.Source{}, false
	}
	abbr := strings.TrimSpace(r.PathValue("abbr"))
	if !sourceAbbrPattern.MatchString(abbr) {
		writeError(w, http.StatusBadRequest, "invalid source abbreviation")
		return repo.Source{}, false
	}
	var (
		source repo.Source
		err    error
	)
	if restore {
		source, err = s.Sources.Restore(r.Context(), abbr)
	} else {
		source, err = s.Sources.Delete(r.Context(), abbr)
	}
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "mutate source failed", "source_abbr", abbr, "restore", restore, "error", err)
		writeError(w, http.StatusNotFound, "source not found")
		return repo.Source{}, false
	}
	return source, true
}

func sourceParams(abbr string, req sourceRequest) repo.CreateSourceParams {
	return repo.CreateSourceParams{Abbr: abbr, Name: strings.TrimSpace(req.Name), Type: strings.TrimSpace(req.Type), BaseURL: strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")}
}

func validateSource(abbr string, req sourceRequest) error {
	if !sourceAbbrPattern.MatchString(abbr) {
		return fmt.Errorf("invalid source abbreviation")
	}
	if name := strings.TrimSpace(req.Name); name == "" || len(name) > 128 {
		return fmt.Errorf("source name is required and must be at most 128 characters")
	}
	if req.Type != repo.SourceTypeParty && req.Type != repo.SourceTypeMedia {
		return fmt.Errorf("source type must be PARTY or MEDIA")
	}
	u, err := url.Parse(strings.TrimSpace(req.BaseURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("base_url must be an https URL")
	}
	return nil
}
