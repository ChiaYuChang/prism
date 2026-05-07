// Package api implements the Prism user-facing HTTP handlers.
//
// Routes are versioned under /api/v1. Handlers are thin adapters over
// repo.Scout / repo.Tasks / repo.Pipeline — validation and response shaping
// live here; persistence and task semantics live in the repo layer.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ChiaYuChang/prism/internal/repo"
)

var ErrParamMissing = errors.New("param missing")

// Server groups dependencies shared by all API handlers.
type Server struct {
	Logger      *slog.Logger
	Scout       repo.Scout
	Tasks       repo.Tasks
	Pipeline    repo.Pipeline
	UserFetches repo.UserFetches
}

// NewServer validates dependencies and returns a ready-to-register Server.
func NewServer(logger *slog.Logger, scout repo.Scout, tasks repo.Tasks, pipeline repo.Pipeline, userFetches repo.UserFetches) (*Server, error) {
	if logger == nil {
		return nil, fmt.Errorf("%w: logger", ErrParamMissing)
	}
	if scout == nil {
		return nil, fmt.Errorf("%w: scout", ErrParamMissing)
	}
	if tasks == nil {
		return nil, fmt.Errorf("%w: tasks", ErrParamMissing)
	}
	if pipeline == nil {
		return nil, fmt.Errorf("%w: pipeline", ErrParamMissing)
	}
	if userFetches == nil {
		return nil, fmt.Errorf("%w: userFetches", ErrParamMissing)
	}
	return &Server{Logger: logger, Scout: scout, Tasks: tasks, Pipeline: pipeline, UserFetches: userFetches}, nil
}

// Register wires v1 routes onto the supplied mux under the /api/v1 prefix.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/candidates", s.ListCandidates)
	mux.HandleFunc("POST /api/v1/page_fetch", s.PageFetch)
	mux.HandleFunc("GET /api/v1/contents/{candidate_id}", s.GetContent)
	mux.HandleFunc("GET /api/v1/fetches/{id}", s.GetFetch)
}

// ErrorResponse is the standard JSON error body.
type ErrorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}
