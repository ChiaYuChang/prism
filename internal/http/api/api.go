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

	authtoken "github.com/ChiaYuChang/prism/internal/auth/token"
	httpclient "github.com/ChiaYuChang/prism/internal/http/client"
	"github.com/ChiaYuChang/prism/internal/http/middleware"
	"github.com/ChiaYuChang/prism/internal/infra"
	"github.com/ChiaYuChang/prism/internal/infra/natsadmin"
	"github.com/ChiaYuChang/prism/internal/infra/natsdiag"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/storage"
)

var ErrParamMissing = errors.New("param missing")

type Monitor struct {
	mode     string
	mu       sync.RWMutex
	statuses map[string]obs.HealthStatus
}

// StatusMonitor stores service health snapshots for GET/POST status routes.
type StatusMonitor interface {
	Mode() string
	SetMode(mode string)
	InitializeStatuses(ctx context.Context, services []string) error
	SetStatus(ctx context.Context, service string, status obs.HealthStatus) error
	Statuses(ctx context.Context) (map[string]obs.HealthStatus, error)
}

// NewInMemoryMonitor returns the default process-local status monitor.
func NewInMemoryMonitor(mode string) *Monitor {
	return &Monitor{
		mode:     mode,
		statuses: make(map[string]obs.HealthStatus),
	}
}

func (m *Monitor) SetMode(mode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mode = mode
}

func (m *Monitor) InitializeStatuses(_ context.Context, services []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, name := range services {
		if _, ok := m.statuses[name]; ok {
			continue
		}
		m.statuses[name] = obs.HealthStatus{
			Level:     obs.LevelStarting,
			Message:   "Waiting for first heartbeat",
			Timestamp: time.Now(),
		}
	}
	return nil
}

func (m *Monitor) SetStatus(_ context.Context, service string, status obs.HealthStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses[service] = status
	return nil
}

func (m *Monitor) StatusesSnapshot(_ context.Context) (map[string]obs.HealthStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	statuses := make(map[string]obs.HealthStatus, len(m.statuses))
	for service, status := range m.statuses {
		statuses[service] = status
	}
	return statuses, nil
}

func (m *Monitor) Statuses(ctx context.Context) (map[string]obs.HealthStatus, error) {
	return m.StatusesSnapshot(ctx)
}

func (m *Monitor) CurrentMode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mode
}

func (m *Monitor) Mode() string { return m.CurrentMode() }

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
		s.Monitor.SetMode(mode)
	}
}

// WithStatusMonitor attaches a status monitor backend. When unset, the server
// uses an in-memory backend.
func WithStatusMonitor(m StatusMonitor) ServerOption {
	return func(s *Server) {
		if m != nil {
			s.Monitor = m
		}
	}
}

// WithOperator attaches the operator read repository used by authenticated
// inspection endpoints.
func WithOperator(operator repo.Operator) ServerOption {
	return func(s *Server) {
		s.Operator = operator
	}
}

// WithPrompts attaches the prompt version repository and storage used by
// authenticated prompt management endpoints.
func WithPrompts(prompts repo.Prompts, store storage.Store) ServerOption {
	return func(s *Server) {
		if prompts != nil {
			s.Prompts = prompts
		}
		if store != nil {
			s.PromptStore = store
		}
	}
}

func WithTokens(tokens repo.Tokens, hasher authtoken.Hasher, tokenTypes map[string]TokenTypeConfig) ServerOption {
	return func(s *Server) {
		s.Tokens = tokens
		s.TokenHasher = hasher
		s.TokenTypes = tokenTypes
	}
}

func WithSchedulerToggles(toggles *infra.SchedulerToggleStore) ServerOption {
	return func(s *Server) { s.SchedulerToggles = toggles }
}

func WithSources(sources repo.Sources) ServerOption {
	return func(s *Server) { s.Sources = sources }
}

// WithNATSInspector attaches the read-only JetStream inspector used by admin
// diagnostics. It cannot publish, subscribe, acknowledge, or mutate NATS.
func WithNATSInspector(inspector NATSInspector) ServerOption {
	return func(s *Server) { s.NATSInspector = inspector }
}

func WithNATSAdmin(admin NATSAdmin) ServerOption {
	return func(s *Server) { s.NATSAdmin = admin }
}

type NATSInspector interface {
	Snapshot(context.Context) (natsdiag.Snapshot, error)
}

type NATSAdmin interface {
	Apply(context.Context, natsadmin.Manifest, natsadmin.ApplyOptions) (natsadmin.ApplyReport, error)
	PauseConsumer(context.Context, natsadmin.ConsumerRef, natsadmin.PauseRequest) (natsadmin.PauseResult, error)
	ResumeConsumer(context.Context, natsadmin.ConsumerRef) (natsadmin.PauseResult, error)
	DeleteConsumer(context.Context, natsadmin.ConsumerRef) error
	DeleteStream(context.Context, string) error
	PurgeStream(context.Context, string) error
	PublishTestMessage(context.Context, string) (natsadmin.TestMessageResult, error)
}

// Server groups dependencies shared by all API handlers.
type Server struct {
	Logger           *slog.Logger
	Scout            repo.Scout
	Tasks            repo.Tasks
	Pipeline         repo.Pipeline
	UserFetches      repo.UserFetches
	Sources          repo.Sources
	Operator         repo.Operator
	Prompts          repo.Prompts
	Tokens           repo.Tokens
	TokenHasher      authtoken.Hasher
	TokenTypes       map[string]TokenTypeConfig
	Cache            ProgressCache
	GetFetchLimiter  middleware.IPLimiter
	Monitor          StatusMonitor
	PromptStore      storage.Store
	SchedulerToggles *infra.SchedulerToggleStore
	NATSInspector    NATSInspector
	NATSAdmin        NATSAdmin
}

type TokenTypeConfig struct {
	Prefix     string
	DefaultTTL time.Duration
	MaxTTL     time.Duration
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
		Monitor:         NewInMemoryMonitor(""),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// RouteRegistrar is the minimal router surface used by API route registration.
type RouteRegistrar interface {
	Handle(pattern string, handler http.Handler)
}

// RegisterV1 wires public v1 routes onto the supplied router.
//
// The /fetches/{id} route is wrapped in a per-IP rate-limit middleware. When
// no limiter is configured, the wrapping uses NoOpIPLimiter and is effectively
// a passthrough.
func (s *Server) RegisterV1(r RouteRegistrar) {
	r.Handle("GET /candidates", http.HandlerFunc(s.ListCandidates))
	r.Handle("POST /page_fetch", http.HandlerFunc(s.PageFetch))
	r.Handle("GET /contents/{candidate_id}", http.HandlerFunc(s.GetContent))
	r.Handle("GET /fetches/{id}", middleware.RateLimit(s.GetFetchLimiter)(http.HandlerFunc(s.GetFetch)))
	r.Handle("GET /status", http.HandlerFunc(s.GetStatus))
}

// RegisterV1Admin wires authenticated v1 operator routes onto the supplied router.
func (s *Server) RegisterV1Admin(r RouteRegistrar) {
	r.Handle("GET /candidates", s.requireAdmin(http.HandlerFunc(s.ListCandidates)))
	r.Handle("GET /candidates/{id}", s.requireAdmin(http.HandlerFunc(s.GetAdminCandidate)))
	r.Handle("GET /models", s.requireAdmin(http.HandlerFunc(s.ListAdminModels)))
	r.Handle("POST /models", s.requireAdmin(http.HandlerFunc(s.CreateAdminModel)))
	r.Handle("GET /sources", s.requireAdmin(http.HandlerFunc(s.ListAdminSources)))
	r.Handle("GET /batches", s.requireAdmin(http.HandlerFunc(s.ListAdminBatches)))
	r.Handle("GET /entities", s.requireAdmin(http.HandlerFunc(s.ListAdminEntities)))
	r.Handle("GET /schedules", s.requireAdmin(http.HandlerFunc(s.ListAdminSchedules)))
	r.Handle("GET /embedding/{model_name}", s.requireAdmin(http.HandlerFunc(s.ListAdminEmbeddings)))
	r.Handle("GET /tasks", s.requireAdmin(http.HandlerFunc(s.ListAdminTasks)))
	r.Handle("POST /tasks", s.requireAdmin(http.HandlerFunc(s.CreateAdminTask)))
	r.Handle("GET /nats", s.requireAdmin(http.HandlerFunc(s.GetAdminNATS)))
	r.Handle("POST /nats/apply", s.requireAdmin(http.HandlerFunc(s.ApplyAdminNATS)))
	r.Handle("POST /nats/streams/{stream}/purge", s.requireAdmin(http.HandlerFunc(s.PurgeAdminNATSStream)))
	r.Handle("DELETE /nats/streams/{stream}", s.requireAdmin(http.HandlerFunc(s.DeleteAdminNATSStream)))
	r.Handle("POST /nats/streams/{stream}/consumers/{consumer}/pause", s.requireAdmin(http.HandlerFunc(s.PauseAdminNATSConsumer)))
	r.Handle("POST /nats/streams/{stream}/consumers/{consumer}/resume", s.requireAdmin(http.HandlerFunc(s.ResumeAdminNATSConsumer)))
	r.Handle("DELETE /nats/streams/{stream}/consumers/{consumer}", s.requireAdmin(http.HandlerFunc(s.DeleteAdminNATSConsumer)))
	r.Handle("POST /nats/test-message", s.requireAdmin(http.HandlerFunc(s.PublishAdminNATSTestMessage)))
	r.Handle("GET /diagnostics", s.requireAdmin(http.HandlerFunc(s.GetAdminDiagnostics)))
	r.Handle("GET /tasks/{id}", s.requireAdmin(http.HandlerFunc(s.GetAdminTask)))
	r.Handle("POST /tasks/{id}/retry", s.requireAdmin(http.HandlerFunc(s.RetryAdminTask)))
	r.Handle("GET /schedulers/{name}", s.requireAdmin(http.HandlerFunc(s.GetAdminSchedulerToggle)))
	r.Handle("POST /schedulers/{name}/pause", s.requireAdmin(http.HandlerFunc(s.PauseAdminScheduler)))
	r.Handle("POST /schedulers/{name}/resume", s.requireAdmin(http.HandlerFunc(s.ResumeAdminScheduler)))
	r.Handle("GET /schedulers", s.requireAdmin(http.HandlerFunc(s.GetAdminGlobalSchedulerToggle)))
	r.Handle("POST /schedulers/pause", s.requireAdmin(http.HandlerFunc(s.PauseAdminGlobalScheduler)))
	r.Handle("POST /schedulers/resume", s.requireAdmin(http.HandlerFunc(s.ResumeAdminGlobalScheduler)))
	r.Handle("POST /sources", s.requireAdmin(http.HandlerFunc(s.CreateAdminSource)))
	r.Handle("PUT /sources/{abbr}", s.requireAdmin(http.HandlerFunc(s.UpdateAdminSource)))
	r.Handle("DELETE /sources/{abbr}", s.requireAdmin(http.HandlerFunc(s.DeleteAdminSource)))
	r.Handle("POST /sources/{abbr}/restore", s.requireAdmin(http.HandlerFunc(s.RestoreAdminSource)))
	r.Handle("GET /prompts", s.requireAdmin(http.HandlerFunc(s.ListPromptVersions)))
	r.Handle("POST /prompts", s.requireAdmin(http.HandlerFunc(s.CreatePromptVersion)))
	r.Handle("GET /prompts/{id}", s.requireAdmin(http.HandlerFunc(s.GetPromptVersion)))
	r.Handle("GET /tokens", s.requireAdmin(http.HandlerFunc(s.ListTokens)))
	r.Handle("GET /tokens/{id}", s.requireAdmin(http.HandlerFunc(s.GetToken)))
}

// RegisterInternal wires private routes for internal administration/push telemetry.
func (s *Server) RegisterInternal(mux *http.ServeMux, mws ...middleware.Middleware) {
	wrap := middleware.Chain(mws...)
	if s.Monitor.Mode() == "push" {
		mux.Handle("POST /api/v1/status", wrap(http.HandlerFunc(s.PostStatus)))
	}
}

// InitializeStatuses registers expected service names and sets their initial health status to LevelStarting.
func (s *Server) InitializeStatuses(services []string) {
	if err := s.Monitor.InitializeStatuses(context.Background(), services); err != nil {
		s.Logger.Error("failed to initialize monitor statuses", "error", err)
	}
}

// ErrorResponse is the standard JSON error body.
type ErrorResponse struct {
	Error string `json:"error"`
}

// StatusRecordResponse is returned after a push-mode status update is stored.
type StatusRecordResponse struct {
	Status string `json:"status"`
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

	client := &http.Client{Transport: httpclient.NewTracingTransport(http.DefaultTransport)}

	pingFunc := func() {
		var wg sync.WaitGroup
		for name, target := range targets {
			wg.Add(1)
			go func(n string, t MonitorTarget) {
				defer wg.Done()
				status := s.pingTarget(ctx, client, t.URL, t.Timeout)
				if err := s.Monitor.SetStatus(ctx, n, status); err != nil {
					s.Logger.Error("failed to store monitor status", "service", n, "error", err)
				}
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
//
// @Summary   List monitored service statuses
// @Tags      status
// @Produce   json
// @Success   200 {object} map[string]obs.HealthStatus
// @Failure   500 {object} ErrorResponse
// @Router    /status [get]
func (s *Server) GetStatus(w http.ResponseWriter, r *http.Request) {
	statuses, err := s.Monitor.Statuses(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load status")
		return
	}
	writeJSON(w, http.StatusOK, statuses)
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
//
// @Summary   Record monitored service status
// @Tags      status
// @Accept    json
// @Produce   json
// @Param     body body PostStatusPayload true "Service status payload"
// @Success   200 {object} StatusRecordResponse
// @Failure   400 {object} ErrorResponse
// @Failure   500 {object} ErrorResponse
// @Router    /status [post]
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

	status := obs.HealthStatus{
		Level:     payload.Level,
		Message:   payload.Message,
		Uptime:    payload.Uptime,
		Timestamp: timestamp,
	}
	if err := s.Monitor.SetStatus(r.Context(), payload.Service, status); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}
