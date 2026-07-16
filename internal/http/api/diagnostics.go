package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ChiaYuChang/prism/internal/infra/natsdiag"
	"github.com/ChiaYuChang/prism/internal/obs"
	"github.com/ChiaYuChang/prism/internal/repo"
)

type AdminDiagnostics struct {
	Services    map[string]obs.HealthStatus `json:"services"`
	NATS        *natsdiag.Snapshot          `json:"nats,omitempty"`
	NATSError   string                      `json:"nats_error,omitempty"`
	TaskSummary []AdminTaskStatusSummary    `json:"task_summary"`
	Failures    []AdminFailedTaskSummary    `json:"recent_failures"`
}

type AdminTaskStatusSummary struct {
	Kind   string          `json:"kind"`
	Status repo.TaskStatus `json:"status"`
	Count  int64           `json:"count"`
}

type AdminFailedTaskSummary struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"`
	SourceAbbr     string  `json:"source_abbr"`
	URL            string  `json:"url"`
	FailureMessage *string `json:"failure_message,omitempty"`
	UpdatedAt      string  `json:"updated_at"`
}

// GetAdminDiagnostics handles GET /api/v1/admin/diagnostics.
//
// @Summary   Show aggregate operator diagnostics
// @Tags      admin
// @Produce   json
// @Param     failure_limit query int false "Number of recent failures"
// @Success   200 {object} AdminDiagnostics
// @Failure   500 {object} ErrorResponse
// @Router    /admin/diagnostics [get]
func (s *Server) GetAdminDiagnostics(w http.ResponseWriter, r *http.Request) {
	services, err := s.Monitor.Statuses(r.Context())
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "load diagnostics service status failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to load service status")
		return
	}
	taskSummary, err := s.Tasks.ListTaskStatusSummary(r.Context())
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "load diagnostics task summary failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to load task summary")
		return
	}
	limit := int32(10)
	if raw := r.URL.Query().Get("failure_limit"); raw != "" {
		value, parseErr := strconv.ParseInt(raw, 10, 32)
		if parseErr != nil || value < 0 || value > 100 {
			writeError(w, http.StatusBadRequest, "failure_limit must be between 0 and 100")
			return
		}
		limit = int32(value)
	}
	failures, err := s.Tasks.ListRecentFailedTasks(r.Context(), limit)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "load diagnostics failures failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to load recent failures")
		return
	}
	taskItems := make([]AdminTaskStatusSummary, len(taskSummary))
	for i, item := range taskSummary {
		taskItems[i] = AdminTaskStatusSummary{Kind: item.Kind, Status: item.Status, Count: item.Count}
	}
	failureItems := make([]AdminFailedTaskSummary, len(failures))
	for i, item := range failures {
		failureItems[i] = AdminFailedTaskSummary{
			ID: item.ID.String(), Kind: item.Kind, SourceAbbr: item.SourceAbbr,
			URL: item.URL, FailureMessage: item.FailureMessage,
			UpdatedAt: item.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		}
	}
	result := AdminDiagnostics{Services: services, TaskSummary: taskItems, Failures: failureItems}
	if s.NATSInspector != nil {
		snapshot, natsErr := s.NATSInspector.Snapshot(r.Context())
		if natsErr != nil {
			result.NATSError = natsErr.Error()
		} else {
			result.NATS = &snapshot
		}
	}
	writeJSON(w, http.StatusOK, result)
}
