// Package api implements the Prism user-facing HTTP handlers.
//
// Routes are versioned under /api/v1. Handlers are thin adapters over
// repo.Scout / repo.Tasks / repo.Pipeline — validation and response shaping
// live here; persistence and task semantics live in the repo layer.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ChiaYuChang/prism/internal/http/middleware"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
)

var ErrParamMissing = errors.New("param missing")

type Monitor struct {
	Mode     string
	mu       sync.RWMutex
	Statuses map[string]obs.HealthStatus
}

type MonitorTarget struct {
	URL         string        `mapstructure:"url"            validate:"required,url"`
	DisplayName string        `mapstructure:"display-name"`
	Description string        `mapstructure:"description"`
	Group       string        `mapstructure:"group"`
	Timeout     time.Duration `mapstructure:"timeout"        validate:"omitempty,min=100ms"`
}

func (t MonitorTarget) Normalized(defaultTimeout time.Duration) MonitorTarget {
	t.URL = strings.TrimSpace(t.URL)
	t.DisplayName = strings.TrimSpace(t.DisplayName)
	t.Description = strings.TrimSpace(t.Description)
	t.Group = strings.TrimSpace(t.Group)
	if t.Group == "" {
		t.Group = "default"
	}
	if t.Timeout == 0 {
		t.Timeout = defaultTimeout
	}
	return t
}

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

// WithMonitorMode configures the server's status monitoring mode ("pull" or "push").
func WithMonitorMode(mode string) ServerOption {
	return func(s *Server) {
		s.Monitor.Mode = mode
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
	Monitor         *Monitor
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
		Monitor: &Monitor{
			Statuses: make(map[string]obs.HealthStatus),
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// RegisterPublic wires public v1 routes onto the supplied mux under the /api/v1 prefix.
//
// The /fetches/{id} route is wrapped in a per-IP rate-limit middleware. When
// no limiter is configured, the wrapping uses NoOpIPLimiter and is effectively
// a passthrough.
func (s *Server) RegisterPublic(mux *http.ServeMux, mws ...middleware.Middleware) {
	wrap := middleware.Chain(mws...)
	mux.Handle("GET /api/v1/candidates", wrap(http.HandlerFunc(s.ListCandidates)))
	mux.Handle("POST /api/v1/page_fetch", wrap(http.HandlerFunc(s.PageFetch)))
	mux.Handle("GET /api/v1/contents/{candidate_id}", wrap(http.HandlerFunc(s.GetContent)))
	mux.Handle("GET /api/v1/fetches/{id}",
		wrap(middleware.RateLimit(s.GetFetchLimiter)(http.HandlerFunc(s.GetFetch))))
	mux.Handle("GET /api/v1/status", wrap(http.HandlerFunc(s.GetStatus)))
}

// RegisterInternal wires private routes for internal administration/push telemetry.
func (s *Server) RegisterInternal(mux *http.ServeMux, mws ...middleware.Middleware) {
	wrap := middleware.Chain(mws...)
	if s.Monitor.Mode == "push" {
		mux.Handle("POST /api/v1/status", wrap(http.HandlerFunc(s.PostStatus)))
	}
}

// InitializeStatuses registers expected service names and sets their initial health status to LevelStarting.
func (s *Server) InitializeStatuses(services []string) {
	s.Monitor.mu.Lock()
	defer s.Monitor.mu.Unlock()
	for _, name := range services {
		s.Monitor.Statuses[name] = obs.HealthStatus{
			Level:     obs.LevelStarting,
			Message:   "Waiting for first heartbeat",
			Timestamp: time.Now(),
		}
	}
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

// StartMonitor starts a background goroutine that periodically pings targets
// for health updates and caches them.
func (s *Server) StartMonitor(ctx context.Context, interval time.Duration, targets map[string]MonitorTarget) {
	if len(targets) == 0 {
		return
	}

	client := &http.Client{}

	pingFunc := func() {
		var wg sync.WaitGroup
		for name, target := range targets {
			wg.Add(1)
			go func(n string, t MonitorTarget) {
				defer wg.Done()
				status := s.pingTarget(ctx, client, t.URL, t.Timeout)

				s.Monitor.mu.Lock()
				s.Monitor.Statuses[n] = status
				s.Monitor.mu.Unlock()
			}(name, target)
		}
		wg.Wait()
	}

	// Run initially on start
	pingFunc()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingFunc()
			}
		}
	}()
}

func (s *Server) pingTarget(ctx context.Context, client *http.Client, url string, timeout time.Duration) obs.HealthStatus {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return obs.HealthStatus{
			Level:     obs.LevelError,
			Message:   fmt.Sprintf("create request: %v", err),
			Timestamp: time.Now(),
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return obs.HealthStatus{
			Level:     obs.LevelError,
			Message:   fmt.Sprintf("ping failed: %v", err),
			Timestamp: time.Now(),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return obs.HealthStatus{
			Level:     obs.LevelError,
			Message:   fmt.Sprintf("status code: %d", resp.StatusCode),
			Timestamp: time.Now(),
		}
	}

	var status obs.HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return obs.HealthStatus{
			Level:     obs.LevelError,
			Message:   fmt.Sprintf("parse response: %v", err),
			Timestamp: time.Now(),
		}
	}

	return status
}

// GetStatus returns the cached health status of monitored services.
func (s *Server) GetStatus(w http.ResponseWriter, r *http.Request) {
	s.Monitor.mu.RLock()
	defer s.Monitor.mu.RUnlock()

	writeJSON(w, http.StatusOK, s.Monitor.Statuses)
}

// PostStatusPayload defines the body structure for updating worker/app status.
type PostStatusPayload struct {
	Service   string          `json:"service"`
	Level     obs.HealthLevel `json:"level"`
	Message   string          `json:"message"`
	Uptime    string          `json:"uptime"`
	Timestamp time.Time       `json:"timestamp"`
}

// PostStatus accepts incoming health status reports (used in push mode).
func (s *Server) PostStatus(w http.ResponseWriter, r *http.Request) {
	var payload PostStatusPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload: "+err.Error())
		return
	}

	if payload.Service == "" {
		writeError(w, http.StatusBadRequest, "service name is required")
		return
	}

	if payload.Level == "" {
		payload.Level = obs.LevelOK
	}

	timestamp := payload.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	s.Monitor.mu.Lock()
	s.Monitor.Statuses[payload.Service] = obs.HealthStatus{
		Level:     payload.Level,
		Message:   payload.Message,
		Uptime:    payload.Uptime,
		Timestamp: timestamp,
	}
	s.Monitor.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}
