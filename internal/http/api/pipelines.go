package api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

type createPipelineRequest struct {
	InputBatchID uuid.UUID `json:"input_batch_id"`
	BatchID      uuid.UUID `json:"batch_id,omitempty"` // legacy alias for input_batch_id
	SourceType   string    `json:"source_type"`
	SourceAbbr   string    `json:"source_abbr"`
	TraceID      string    `json:"trace_id"`
	PipelineFile string    `json:"pipeline_file,omitempty"` // rejected unless it is the deployed default
}

const defaultPipelineFile = "configs/llm_pipeline.yaml"

// CreateAdminPipeline creates a Root batch and its PIPELINE_INIT task in one
// repository transaction. The deployed pipeline definition is fixed to
// configs/llm_pipeline.yaml. The source batch must be terminal (completed_at is
// set); source batches may contain partial collection results. An
// Idempotency-Key makes retries for the same input batch return the existing
// init task, while requests without a key intentionally create new runs.
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
	if req.InputBatchID == uuid.Nil {
		req.InputBatchID = req.BatchID
	}
	if req.InputBatchID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "input_batch_id is required")
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idempotencyKey) > 255 {
		writeError(w, http.StatusBadRequest, "Idempotency-Key is too long")
		return
	}
	if pipelineFile := strings.TrimSpace(req.PipelineFile); pipelineFile != "" && pipelineFile != defaultPipelineFile {
		writeError(w, http.StatusBadRequest, "pipeline_file is not selectable; use the deployed pipeline definition")
		return
	}
	pipelineFile := defaultPipelineFile
	pipelineData, err := readPipelineDefinition(pipelineFile)
	if err != nil {
		writeError(w, http.StatusBadRequest, "pipeline definition is unavailable")
		return
	}
	definitionHash := fmt.Sprintf("%x", sha256.Sum256(pipelineData))
	requestFingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join([]string{
		req.InputBatchID.String(), strings.TrimSpace(req.SourceType), strings.TrimSpace(req.SourceAbbr),
		pipelineFile, definitionHash,
	}, "\x00"))))
	var rootID uuid.UUID
	if idempotencyKey != "" {
		// Idempotency is scoped to the source batch. Requests without a key
		// intentionally create a new analysis run.
		rootID = uuid.NewSHA1(uuid.NameSpaceURL, []byte("prism/pipeline/"+req.InputBatchID.String()+"/"+idempotencyKey))
	} else {
		rootID, err = uuid.NewV7()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create pipeline root id")
			return
		}
	}
	req.BatchID = rootID
	if strings.TrimSpace(req.SourceType) == "" || strings.TrimSpace(req.SourceAbbr) == "" || strings.TrimSpace(req.TraceID) == "" {
		writeError(w, http.StatusBadRequest, "source_type, source_abbr, and trace_id are required")
		return
	}
	payload, err := json.Marshal(map[string]string{"pipeline_file": pipelineFile, "pipeline_definition_hash": definitionHash})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pipeline payload")
		return
	}
	var executionID *uuid.UUID
	if s.Reports != nil {
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("prism/execution/v1/"+requestFingerprint))
		if err := s.Reports.EnsureExecution(r.Context(), id, requestFingerprint); err != nil {
			s.Logger.ErrorContext(r.Context(), "ensure pipeline execution failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create pipeline execution")
			return
		}
		executionID = &id
	}
	logicalKey := "pipeline:init"
	task, err := s.PipelineRuntime.CreatePipelineRoot(r.Context(), repo.CreateTaskParams{
		BatchID: rootID, AnalysisExecutionID: executionID, ParentBatchID: &req.InputBatchID, Kind: repo.TaskKindPipelineInit, SourceType: strings.TrimSpace(req.SourceType),
		SourceAbbr: strings.TrimSpace(req.SourceAbbr), URL: "pipeline://init/" + rootID.String(),
		Payload: payload, TraceID: strings.TrimSpace(req.TraceID), LogicalKey: &logicalKey,
		PipelineDefinitionHash: definitionHash, PipelineIdempotencyKey: optionalString(idempotencyKey),
		PipelineRequestFingerprint: requestFingerprint,
	})
	if err != nil {
		if errors.Is(err, repo.ErrTaskAlreadyActive) && task.ID != uuid.Nil {
			writeJSON(w, http.StatusOK, toAdminTask(task))
			return
		}
		if errors.Is(err, repo.ErrPipelineInputNotTerminal) {
			writeError(w, http.StatusConflict, "input batch is not completed")
			return
		}
		if errors.Is(err, repo.ErrPipelineInputFailed) {
			writeError(w, http.StatusConflict, "input batch failed")
			return
		}
		if errors.Is(err, repo.ErrPipelineIdempotencyConflict) {
			writeError(w, http.StatusConflict, "idempotency key conflicts with an existing request")
			return
		}
		if errors.Is(err, repo.ErrPipelineInputNotFound) {
			writeError(w, http.StatusNotFound, "input batch not found")
			return
		}
		s.Logger.ErrorContext(r.Context(), "create pipeline failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create pipeline")
		return
	}
	writeJSON(w, http.StatusCreated, toAdminTask(task))
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func readPipelineDefinition(filename string) ([]byte, error) {
	if data, err := os.ReadFile(filename); err == nil {
		return data, nil
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for dir := workingDir; ; dir = filepath.Dir(dir) {
		data, readErr := os.ReadFile(filepath.Join(dir, filename))
		if readErr == nil {
			return data, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, readErr
		}
	}
}
