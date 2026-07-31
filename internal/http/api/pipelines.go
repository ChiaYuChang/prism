package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

type createPipelineRequest struct {
	BatchID      uuid.UUID `json:"batch_id"`
	SourceType   string    `json:"source_type"`
	SourceAbbr   string    `json:"source_abbr"`
	TraceID      string    `json:"trace_id"`
	PipelineFile string    `json:"pipeline_file,omitempty"`
}

// CreateAdminPipeline creates a Root batch and its PIPELINE_INIT task in one
// repository transaction. The pipeline worker loads the configured definition
// file; the request field is retained as task payload for future versioning.
func (s *Server) CreateAdminPipeline(w http.ResponseWriter, r *http.Request) {
	if s.PipelineRuntime == nil {
		writeError(w, http.StatusServiceUnavailable, "pipeline runtime is unavailable")
		return
	}
	var req createPipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.BatchID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create batch id")
			return
		}
		req.BatchID = id
	}
	if strings.TrimSpace(req.SourceType) == "" || strings.TrimSpace(req.SourceAbbr) == "" || strings.TrimSpace(req.TraceID) == "" {
		writeError(w, http.StatusBadRequest, "source_type, source_abbr, and trace_id are required")
		return
	}
	payload, err := json.Marshal(map[string]string{"pipeline_file": strings.TrimSpace(req.PipelineFile)})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pipeline payload")
		return
	}
	logicalKey := "pipeline:init"
	task, err := s.PipelineRuntime.CreatePipelineRoot(r.Context(), repo.CreateTaskParams{
		BatchID: req.BatchID, Kind: repo.TaskKindPipelineInit, SourceType: strings.TrimSpace(req.SourceType),
		SourceAbbr: strings.TrimSpace(req.SourceAbbr), URL: "pipeline://init/" + req.BatchID.String(),
		Payload: payload, TraceID: strings.TrimSpace(req.TraceID), LogicalKey: &logicalKey,
	})
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "create pipeline failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create pipeline")
		return
	}
	writeJSON(w, http.StatusCreated, toAdminTask(task))
}
