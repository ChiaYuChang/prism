package obs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// HealthLevel defines the severity of the service health.
type HealthLevel string

const (
	LevelStarting HealthLevel = "STARTING"
	LevelOK       HealthLevel = "OK"
	LevelWarn     HealthLevel = "WARN"
	LevelError    HealthLevel = "ERROR"
)

// HealthStatus represents the response for the health check endpoint.
type HealthStatus struct {
	Level     HealthLevel `json:"level"`
	Message   string      `json:"message"`
	Uptime    string      `json:"uptime"`
	Timestamp time.Time   `json:"timestamp"`
}

// HealthMonitor provides a thread-safe way to manage and report service health.
type HealthMonitor struct {
	mu      sync.RWMutex
	level   HealthLevel
	message string
	start   time.Time
}

// NewHealthMonitor initializes a monitor with a "STARTING" level and sets the start time.
func NewHealthMonitor() *HealthMonitor {
	return &HealthMonitor{
		level:   LevelStarting,
		message: "Service is initializing",
		start:   time.Now(),
	}
}

// SetStatus updates the current health status with a level and a message.
func (h *HealthMonitor) SetStatus(level HealthLevel, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.level = level
	h.message = message
}

// OK is a convenience method to set the health status to LevelOK.
func (h *HealthMonitor) OK() {
	h.SetStatus(LevelOK, "OK")
}

// Status retrieves the current health level and message.
func (h *HealthMonitor) Status() (HealthLevel, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.level, h.message
}

// Uptime returns the duration since the monitor was created.
func (h *HealthMonitor) Uptime() time.Duration {
	return time.Since(h.start)
}

// StartHealthServer starts a minimal HTTP server on the specified port for Docker health checks.
// It uses the provided monitor to report the current status.
func StartHealthServer(ctx context.Context, port int, monitor *HealthMonitor) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		level, message := monitor.Status()

		w.Header().Set("Content-Type", "application/json")
		if level == LevelOK {
			w.WriteHeader(http.StatusOK)
		} else {
			// Return 503 if service is not in OK state
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		if err := json.NewEncoder(w).Encode(HealthStatus{
			Level:     level,
			Message:   message,
			Uptime:    monitor.Uptime().Truncate(time.Second).String(),
			Timestamp: time.Now(),
		}); err != nil {
			slog.Error("Failed to write health response", "error", err.Error())
		}
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Health server error: %v\n", err)
		}
	}()

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
}
