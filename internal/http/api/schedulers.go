package api

import (
	"net/http"
	"strings"
)

type schedulerToggleResponse struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// GetAdminGlobalSchedulerToggle handles GET /api/v1/admin/schedulers.
func (s *Server) GetAdminGlobalSchedulerToggle(w http.ResponseWriter, r *http.Request) {
	s.getAdminSchedulerToggle(w, r, "")
}

// PauseAdminGlobalScheduler handles POST /api/v1/admin/schedulers/pause.
func (s *Server) PauseAdminGlobalScheduler(w http.ResponseWriter, r *http.Request) {
	s.setAdminGlobalSchedulerToggle(w, r, false)
}

// ResumeAdminGlobalScheduler handles POST /api/v1/admin/schedulers/resume.
func (s *Server) ResumeAdminGlobalScheduler(w http.ResponseWriter, r *http.Request) {
	s.setAdminGlobalSchedulerToggle(w, r, true)
}

// GetAdminSchedulerToggle handles GET /api/v1/admin/schedulers/{name}.
func (s *Server) GetAdminSchedulerToggle(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	s.getAdminSchedulerToggle(w, r, name)
}

func (s *Server) getAdminSchedulerToggle(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" {
		name = "global"
	}
	if s.SchedulerToggles == nil {
		writeError(w, http.StatusServiceUnavailable, "scheduler controls are disabled")
		return
	}
	var enabled bool
	var err error
	if name == "global" {
		enabled, err = s.SchedulerToggles.GlobalEnabled(r.Context())
	} else {
		enabled, err = s.SchedulerToggles.Enabled(r.Context(), name)
	}
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "read scheduler toggle failed", "scheduler", name, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read scheduler toggle")
		return
	}
	writeJSON(w, http.StatusOK, schedulerToggleResponse{Name: name, Enabled: enabled})
}

// PauseAdminScheduler handles POST /api/v1/admin/schedulers/{name}/pause.
func (s *Server) PauseAdminScheduler(w http.ResponseWriter, r *http.Request) {
	s.setAdminSchedulerToggle(w, r, false)
}

// ResumeAdminScheduler handles POST /api/v1/admin/schedulers/{name}/resume.
func (s *Server) ResumeAdminScheduler(w http.ResponseWriter, r *http.Request) {
	s.setAdminSchedulerToggle(w, r, true)
}

func (s *Server) setAdminSchedulerToggle(w http.ResponseWriter, r *http.Request, enabled bool) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "scheduler name is required")
		return
	}
	if s.SchedulerToggles == nil {
		writeError(w, http.StatusServiceUnavailable, "scheduler controls are disabled")
		return
	}
	if err := s.SchedulerToggles.SetScheduler(r.Context(), name, enabled); err != nil {
		s.Logger.ErrorContext(r.Context(), "set scheduler toggle failed", "scheduler", name, "enabled", enabled, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update scheduler toggle")
		return
	}
	writeJSON(w, http.StatusOK, schedulerToggleResponse{Name: name, Enabled: enabled})
}

func (s *Server) setAdminGlobalSchedulerToggle(w http.ResponseWriter, r *http.Request, enabled bool) {
	if s.SchedulerToggles == nil {
		writeError(w, http.StatusServiceUnavailable, "scheduler controls are disabled")
		return
	}
	if err := s.SchedulerToggles.SetGlobal(r.Context(), enabled); err != nil {
		s.Logger.ErrorContext(r.Context(), "set global scheduler toggle failed", "enabled", enabled, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update global scheduler toggle")
		return
	}
	writeJSON(w, http.StatusOK, schedulerToggleResponse{Name: "global", Enabled: enabled})
}
