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

	"github.com/ChiaYuChang/prism/internal/http/middleware"
	"github.com/ChiaYuChang/prism/internal/repo"
)

var ErrParamMissing = errors.New("param missing")

// ServerOption configures optional dependencies on a Server.
type ServerOption func(*Server)

// WithProgressCache attaches a ProgressCache used by GetFetch. When unset, a
// NoOpProgressCache is used (no caching).
func WithProgressCache(c ProgressCache) ServerOption {
	return func(s *Server) {
		if c != nil {
			s.Cache = c
		}
	}
}

// WithGetFetchLimiter attaches a per-IP rate limiter applied to GET /fetches/{id}.
// When unset, no rate limiting is applied.
func WithGetFetchLimiter(l middleware.IPLimiter) ServerOption {
	return func(s *Server) {
		if l != nil {
			s.GetFetchLimiter = l
		}
	}
}

// Server groups dependencies shared by all API handlers.
type Server struct {
	Logger          *slog.Logger
	Scout           repo.Scout
	Tasks           repo.Tasks
	Pipeline        repo.Pipeline
	UserFetches     repo.UserFetches
	Cache           ProgressCache
	GetFetchLimiter middleware.IPLimiter
}

// NewServer validates dependencies and returns a ready-to-register Server.
func NewServer(logger *slog.Logger, scout repo.Scout, tasks repo.Tasks, pipeline repo.Pipeline, userFetches repo.UserFetches, opts ...ServerOption) (*Server, error) {
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
	s := &Server{
		Logger:          logger,
		Scout:           scout,
		Tasks:           tasks,
		Pipeline:        pipeline,
		UserFetches:     userFetches,
		Cache:           NoOpProgressCache{},
		GetFetchLimiter: middleware.NoOpIPLimiter{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Register wires v1 routes onto the supplied mux under the /api/v1 prefix.
//
// The /fetches/{id} route is wrapped in a per-IP rate-limit middleware. When
// no limiter is configured, the wrapping uses NoOpIPLimiter and is effectively
// a passthrough.
func (s *Server) Register(mux *http.ServeMux, mws ...middleware.Middleware) {
	wrap := middleware.Chain(mws...)
	mux.Handle("GET /api/v1/candidates", wrap(http.HandlerFunc(s.ListCandidates)))
	mux.Handle("POST /api/v1/page_fetch", wrap(http.HandlerFunc(s.PageFetch)))
	mux.Handle("GET /api/v1/contents/{candidate_id}", wrap(http.HandlerFunc(s.GetContent)))
	mux.Handle("GET /api/v1/fetches/{id}",
		wrap(middleware.RateLimit(s.GetFetchLimiter)(http.HandlerFunc(s.GetFetch))))
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
