package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/ChiaYuChang/prism/internal/infra/natsadmin"
)

// GetAdminNATS handles GET /api/v1/admin/nats.
//
// @Summary   Inspect NATS JetStream state
// @Tags      admin
// @Produce   json
// @Success   200 {object} natsdiag.Snapshot
// @Failure   503 {object} ErrorResponse
// @Router    /admin/nats [get]
func (s *Server) GetAdminNATS(w http.ResponseWriter, r *http.Request) {
	if s.NATSInspector == nil {
		writeError(w, http.StatusServiceUnavailable, "nats diagnostics unavailable")
		return
	}
	snapshot, err := s.NATSInspector.Snapshot(r.Context())
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "inspect nats failed", slog.Any("error", err))
		writeError(w, http.StatusServiceUnavailable, "nats diagnostics failed")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

type ApplyNATSRequest struct {
	Manifest natsadmin.Manifest `json:"manifest"`
	DryRun   bool               `json:"dry_run"`
}

// ApplyAdminNATS handles POST /api/v1/admin/nats/apply.
//
// @Summary Apply a JetStream manifest
// @Tags admin
// @Accept json
// @Produce json
// @Param request body ApplyNATSRequest true "JetStream manifest"
// @Success 200 {object} natsadmin.ApplyReport
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Router /admin/nats/apply [post]
func (s *Server) ApplyAdminNATS(w http.ResponseWriter, r *http.Request) {
	if s.NATSAdmin == nil {
		writeError(w, http.StatusServiceUnavailable, "nats administration unavailable")
		return
	}
	var req ApplyNATSRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := req.Manifest.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	report, err := s.NATSAdmin.Apply(r.Context(), req.Manifest, natsadmin.ApplyOptions{DryRun: req.DryRun})
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "apply nats manifest failed", slog.Any("error", err))
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

type PauseNATSConsumerRequest struct {
	Until  time.Time `json:"until"`
	Reason string    `json:"reason,omitempty"`
}

// PauseAdminNATSConsumer handles pausing a durable consumer.
//
// @Summary Pause a JetStream consumer
// @Tags admin
// @Accept json
// @Produce json
// @Param stream path string true "Stream name"
// @Param consumer path string true "Consumer name"
// @Param request body PauseNATSConsumerRequest true "Pause deadline"
// @Success 200 {object} natsadmin.PauseResult
// @Failure 400 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Router /admin/nats/streams/{stream}/consumers/{consumer}/pause [post]
func (s *Server) PauseAdminNATSConsumer(w http.ResponseWriter, r *http.Request) {
	if s.NATSAdmin == nil {
		writeError(w, http.StatusServiceUnavailable, "nats administration unavailable")
		return
	}
	var req PauseNATSConsumerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	result, err := s.NATSAdmin.PauseConsumer(r.Context(), natsConsumerRef(r), natsadmin.PauseRequest{Until: req.Until, Reason: req.Reason})
	if err != nil {
		writeNATSAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ResumeAdminNATSConsumer handles resuming a paused durable consumer.
//
// @Summary Resume a JetStream consumer
// @Tags admin
// @Produce json
// @Param stream path string true "Stream name"
// @Param consumer path string true "Consumer name"
// @Success 200 {object} natsadmin.PauseResult
// @Failure 502 {object} ErrorResponse
// @Router /admin/nats/streams/{stream}/consumers/{consumer}/resume [post]
func (s *Server) ResumeAdminNATSConsumer(w http.ResponseWriter, r *http.Request) {
	if s.NATSAdmin == nil {
		writeError(w, http.StatusServiceUnavailable, "nats administration unavailable")
		return
	}
	result, err := s.NATSAdmin.ResumeConsumer(r.Context(), natsConsumerRef(r))
	if err != nil {
		writeNATSAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// DeleteAdminNATSConsumer handles deleting a durable consumer.
//
// @Summary Delete a JetStream consumer
// @Tags admin
// @Param stream path string true "Stream name"
// @Param consumer path string true "Consumer name"
// @Param confirm query string true "Consumer name confirmation"
// @Success 204
// @Failure 400 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Router /admin/nats/streams/{stream}/consumers/{consumer} [delete]
func (s *Server) DeleteAdminNATSConsumer(w http.ResponseWriter, r *http.Request) {
	if s.NATSAdmin == nil {
		writeError(w, http.StatusServiceUnavailable, "nats administration unavailable")
		return
	}
	ref := natsConsumerRef(r)
	if r.URL.Query().Get("confirm") != ref.Consumer {
		writeError(w, http.StatusBadRequest, "consumer confirmation does not match")
		return
	}
	if err := s.NATSAdmin.DeleteConsumer(r.Context(), ref); err != nil {
		writeNATSAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PurgeAdminNATSStream handles purging all retained stream messages.
//
// @Summary Purge a JetStream stream
// @Tags admin
// @Param stream path string true "Stream name"
// @Param confirm query string true "Stream name confirmation"
// @Success 204
// @Failure 400 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Router /admin/nats/streams/{stream}/purge [post]
func (s *Server) PurgeAdminNATSStream(w http.ResponseWriter, r *http.Request) {
	if s.NATSAdmin == nil {
		writeError(w, http.StatusServiceUnavailable, "nats administration unavailable")
		return
	}
	stream := r.PathValue("stream")
	if r.URL.Query().Get("confirm") != stream {
		writeError(w, http.StatusBadRequest, "stream confirmation does not match")
		return
	}
	if err := s.NATSAdmin.PurgeStream(r.Context(), stream); err != nil {
		writeNATSAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteAdminNATSStream handles deleting a stream and its consumers.
//
// @Summary Delete a JetStream stream
// @Tags admin
// @Param stream path string true "Stream name"
// @Param confirm query string true "Stream name confirmation"
// @Success 204
// @Failure 400 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Router /admin/nats/streams/{stream} [delete]
func (s *Server) DeleteAdminNATSStream(w http.ResponseWriter, r *http.Request) {
	if s.NATSAdmin == nil {
		writeError(w, http.StatusServiceUnavailable, "nats administration unavailable")
		return
	}
	stream := r.PathValue("stream")
	if r.URL.Query().Get("confirm") != stream {
		writeError(w, http.StatusBadRequest, "stream confirmation does not match")
		return
	}
	if err := s.NATSAdmin.DeleteStream(r.Context(), stream); err != nil {
		writeNATSAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type NATSTestMessageRequest struct {
	Subject string `json:"subject"`
}

// PublishAdminNATSTestMessage handles restricted diagnostic publishing.
//
// @Summary Publish a restricted JetStream test message
// @Tags admin
// @Accept json
// @Produce json
// @Param request body NATSTestMessageRequest true "Test subject"
// @Success 201 {object} natsadmin.TestMessageResult
// @Failure 400 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Router /admin/nats/test-message [post]
func (s *Server) PublishAdminNATSTestMessage(w http.ResponseWriter, r *http.Request) {
	if s.NATSAdmin == nil {
		writeError(w, http.StatusServiceUnavailable, "nats administration unavailable")
		return
	}
	var req NATSTestMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Subject == "" {
		writeError(w, http.StatusBadRequest, "subject is required")
		return
	}
	result, err := s.NATSAdmin.PublishTestMessage(r.Context(), req.Subject)
	if err != nil {
		writeNATSAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func natsConsumerRef(r *http.Request) natsadmin.ConsumerRef {
	return natsadmin.ConsumerRef{Stream: r.PathValue("stream"), Consumer: r.PathValue("consumer")}
}

func writeNATSAdminError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusBadGateway, err.Error())
}
