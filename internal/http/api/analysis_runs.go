package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/ChiaYuChang/prism/pkg/utils"

	"github.com/ChiaYuChang/prism/internal/http/middleware"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxAnalysisCandidates = 100

type analysisSelectionRequest struct {
	CandidateIDs         []uuid.UUID `json:"candidate_ids"`
	SelectedCandidateIDs []uuid.UUID `json:"selected_candidate_ids"`
}

type AnalysisPreflightResponse struct {
	Total                   int         `json:"total"`
	AvailableCandidateIDs   []uuid.UUID `json:"available_candidate_ids"`
	UnavailableCandidateIDs []uuid.UUID `json:"unavailable_candidate_ids"`
}

type AnalysisRunRequest struct {
	AnalysisID uuid.UUID `json:"analysis_id"`
	analysisSelectionRequest
	Topic              string `json:"topic"`
	Brief              string `json:"brief"`
	FetchFailurePolicy string `json:"fetch_failure_policy"`
}

type AnalysisRunResponse struct {
	AnalysisID    uuid.UUID `json:"analysis_id"`
	AnalysisRunID uuid.UUID `json:"analysis_run_id"`
	FetchID       uuid.UUID `json:"fetch_id"`
	Status        string    `json:"status"`
}

type ResolveFetchFailuresRequest struct {
	Action string `json:"action"`
}

func (r analysisSelectionRequest) ids() []uuid.UUID {
	if len(r.SelectedCandidateIDs) > 0 {
		return r.SelectedCandidateIDs
	}
	return r.CandidateIDs
}

func (s *Server) AnalysisPreflight(w http.ResponseWriter, r *http.Request) {
	var req analysisSelectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	rawIDs := req.ids()
	ids, err := validateAnalysisIDs(rawIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(ids) < len(rawIDs) {
		s.Logger.WarnContext(r.Context(), "duplicate candidate_ids removed in preflight", slog.Int("original", len(rawIDs)), slog.Int("unique", len(ids)))
	}
	available, unavailable, err := s.classifyCandidates(r.Context(), ids)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "analysis preflight failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to validate candidates")
		return
	}
	writeJSON(w, http.StatusOK, AnalysisPreflightResponse{
		Total: len(ids), AvailableCandidateIDs: available, UnavailableCandidateIDs: unavailable,
	})
}

func (s *Server) CreateAnalysisRun(w http.ResponseWriter, r *http.Request) {
	if s.AnalysisRuns == nil {
		writeError(w, http.StatusServiceUnavailable, "analysis runs are unavailable")
		return
	}
	var req AnalysisRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.AnalysisID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "analysis_id is required")
		return
	}
	if req.AnalysisID.Version() != 7 {
		writeError(w, http.StatusBadRequest, "analysis_id must be a UUIDv7")
		return
	}
	rawIDs := req.ids()
	ids, err := validateAnalysisIDs(rawIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(ids) < len(rawIDs) {
		s.Logger.WarnContext(r.Context(), "duplicate candidate_ids removed in create session", slog.Int("original", len(rawIDs)), slog.Int("unique", len(ids)))
	}
	policy := strings.ToUpper(strings.TrimSpace(req.FetchFailurePolicy))
	if policy != repo.AnalysisFailurePolicyStop && policy != repo.AnalysisFailurePolicyIgnoreFailed {
		writeError(w, http.StatusBadRequest, "fetch_failure_policy must be STOP or IGNORE_FAILED")
		return
	}
	available, unavailable, err := s.classifyCandidates(r.Context(), ids)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "analysis candidate validation failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to validate candidates")
		return
	}
	if len(unavailable) > 0 {
		writeJSON(w, http.StatusConflict, AnalysisPreflightResponse{
			Total: len(ids), AvailableCandidateIDs: available, UnavailableCandidateIDs: unavailable,
		})
		return
	}
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Idempotency check: if the run already exists, return the existing status
	existingRun, err := s.AnalysisRuns.GetByID(r.Context(), req.AnalysisID)
	if err == nil {
		// Ensure the run belongs to the authenticated user
		if existingRun.UserID == nil || *existingRun.UserID != principal.TokenID {
			writeError(w, http.StatusForbidden, "analysis run belongs to another user")
			return
		}
		writeJSON(w, http.StatusAccepted, analysisRunResponse(existingRun))
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		s.Logger.ErrorContext(r.Context(), "failed to check existing analysis run", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to check existing analysis run")
		return
	}

	runID := req.AnalysisID

	candidates, err := s.Scout.GetCandidatesByIDs(r.Context(), ids)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "load analysis candidates failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to load candidates")
		return
	}

	run, err := s.AnalysisRuns.CreateSession(r.Context(), repo.CreateAnalysisSessionParams{
		AnalysisID:         runID,
		UserID:             &principal.TokenID,
		Topic:              strings.TrimSpace(req.Topic),
		Brief:              strings.TrimSpace(req.Brief),
		FetchFailurePolicy: policy,
		SelectedCandidates: candidates,
	})
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "create analysis session failed", slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "failed to create analysis run session")
		return
	}

	writeJSON(w, http.StatusAccepted, AnalysisRunResponse{
		AnalysisID: run.ID, AnalysisRunID: run.ID, FetchID: run.FetchID, Status: run.Status,
	})
}

func (s *Server) ResolveFetchFailures(w http.ResponseWriter, r *http.Request) {
	if s.AnalysisRuns == nil {
		writeError(w, http.StatusServiceUnavailable, "analysis runs are unavailable")
		return
	}
	run, ok := s.loadAnalysisRun(r.Context(), w, r.PathValue("id"))
	if !ok {
		return
	}
	if run.Status != repo.AnalysisRunStatusAwaitingResolution {
		writeError(w, http.StatusConflict, "analysis run is not awaiting fetch-failure resolution")
		return
	}
	var req ResolveFetchFailuresRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	switch strings.ToUpper(strings.TrimSpace(req.Action)) {
	case "RETRY_FAILED":
		if err := s.AnalysisRuns.RetryFailedItems(r.Context(), run.ID); err != nil {
			s.Logger.ErrorContext(r.Context(), "failed to retry fetch items", slog.Any("error", err))
			writeError(w, http.StatusInternalServerError, "failed to retry fetch items")
			return
		}
		var err error
		run, err = s.AnalysisRuns.SetStatus(r.Context(), repo.SetAnalysisRunStatusParams{
			ID: run.ID, Status: repo.AnalysisRunStatusFetching,
			FailedCandidateIDs: []uuid.UUID{}, // Clear failed IDs as they are now retried
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resume analysis run")
			return
		}
	case "IGNORE_FAILED":
		var err error
		run, err = s.confirmAnalysisRun(r.Context(), run)
		if err != nil {
			s.writeAnalysisTransitionError(w, err)
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "action must be RETRY_FAILED or IGNORE_FAILED")
		return
	}
	writeJSON(w, http.StatusOK, analysisRunResponse(run))
}

func (s *Server) confirmAnalysisRun(ctx context.Context, run repo.AnalysisRun) (repo.AnalysisRun, error) {
	items, err := s.AnalysisRuns.ListItems(ctx, run.FetchID)
	if err != nil {
		return repo.AnalysisRun{}, err
	}
	readyCandidates := make([]uuid.UUID, 0, len(items))
	readyContents := make([]uuid.UUID, 0, len(items))
	failed := make([]uuid.UUID, 0)
	for _, item := range items {
		if item.ContentID != nil && (item.SnapshotStatus != nil && *item.SnapshotStatus == repo.UserFetchItemSnapshotAlreadyComplete || item.TaskStatus != nil && *item.TaskStatus == string(repo.TaskStatusCompleted)) {
			readyCandidates = append(readyCandidates, item.CandidateID)
			readyContents = append(readyContents, *item.ContentID)
			continue
		}
		if item.TaskStatus != nil && (*item.TaskStatus == string(repo.TaskStatusFailed) || *item.TaskStatus == string(repo.TaskStatusCompleted)) {
			failed = append(failed, item.CandidateID)
		}
	}

	if len(readyCandidates) == 0 {
		return s.AnalysisRuns.SetStatus(ctx, repo.SetAnalysisRunStatusParams{
			ID: run.ID, Status: repo.AnalysisRunStatusFailed,
			FailedCandidateIDs: failed, FailureCode: stringPtr("ANALYSIS_NO_READY_INPUT"),
		})
	}
	if len(failed) > 0 && run.FetchFailurePolicy == repo.AnalysisFailurePolicyStop {
		return s.AnalysisRuns.SetStatus(ctx, repo.SetAnalysisRunStatusParams{
			ID: run.ID, Status: repo.AnalysisRunStatusAwaitingResolution,
			FailedCandidateIDs: failed, FailureCode: stringPtr("FETCH_FAILED"),
		})
	}
	manifest, err := s.AnalysisRuns.SetManifest(ctx, repo.SetAnalysisRunManifestParams{
		ID: run.ID, ReadyCandidateIDs: readyCandidates,
		ReadyContentIDs: readyContents, FailedCandidateIDs: failed,
	})
	if err != nil {
		return repo.AnalysisRun{}, err
	}
	return s.startAnalysisRoot(ctx, manifest)
}

func (s *Server) startAnalysisRoot(ctx context.Context, run repo.AnalysisRun) (repo.AnalysisRun, error) {
	if run.Status != repo.AnalysisRunStatusReadyToAnalyze || s.PipelineRuntime == nil {
		return run, nil
	}
	if s.Reports == nil {
		return repo.AnalysisRun{}, errors.New("report repository is unavailable")
	}
	candidates, err := s.Scout.GetCandidatesByIDs(ctx, run.ReadyCandidateIDs)
	if err != nil {
		return repo.AnalysisRun{}, err
	}
	if len(candidates) == 0 || len(run.ReadyContentIDs) == 0 {
		failure := stringPtr("ANALYSIS_NO_READY_INPUT")
		failedRun, setErr := s.AnalysisRuns.SetStatus(ctx, repo.SetAnalysisRunStatusParams{
			ID: run.ID, Status: repo.AnalysisRunStatusFailed,
			FailedCandidateIDs: run.FailedCandidateIDs, FailureCode: failure,
		})
		if setErr != nil {
			return repo.AnalysisRun{}, setErr
		}
		return failedRun, nil
	}
	pipelineData, err := readPipelineDefinition(defaultPipelineFile)
	if err != nil {
		failedRun, setErr := s.AnalysisRuns.SetStatus(ctx, repo.SetAnalysisRunStatusParams{
			ID: run.ID, Status: repo.AnalysisRunStatusFailed,
			FailedCandidateIDs: run.FailedCandidateIDs, FailureCode: stringPtr("ANALYSIS_ROOT_FAILED"),
		})
		if setErr != nil {
			return repo.AnalysisRun{}, fmt.Errorf("read pipeline definition: %w (record failure: %v)", err, setErr)
		}
		return failedRun, nil
	}
	definitionHash := fmt.Sprintf("%x", sha256.Sum256(pipelineData))
	fingerprintData, _ := json.Marshal(struct {
		RunID  uuid.UUID   `json:"run_id"`
		Topic  string      `json:"topic"`
		Brief  string      `json:"brief"`
		Inputs []uuid.UUID `json:"inputs"`
	}{run.ID, run.Topic, run.Brief, run.ReadyContentIDs})
	requestFingerprint := fmt.Sprintf("%x", sha256.Sum256(fingerprintData))
	reportFingerprintData, _ := json.Marshal(struct {
		Topic        string      `json:"topic"`
		Brief        string      `json:"brief"`
		Candidates   []uuid.UUID `json:"candidates"`
		Contents     []uuid.UUID `json:"contents"`
		PipelineHash string      `json:"pipeline_hash"`
	}{run.Topic, run.Brief, run.ReadyCandidateIDs, run.ReadyContentIDs, definitionHash})
	reportFingerprint := fmt.Sprintf("%x", sha256.Sum256(reportFingerprintData))
	executionID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("prism/execution/v1/"+reportFingerprint))
	if run.ExecutionID == nil {
		if err := s.Reports.EnsureExecution(ctx, executionID, reportFingerprint); err != nil {
			return repo.AnalysisRun{}, fmt.Errorf("ensure analysis execution: %w", err)
		}
		run, err = s.AnalysisRuns.SetExecution(ctx, run.ID, executionID)
		if err != nil {
			return repo.AnalysisRun{}, fmt.Errorf("link analysis execution: %w", err)
		}
	}
	rootID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("prism/analysis/"+run.ID.String()))
	traceID := "analysis-run/" + run.ID.String()
	logicalKey := "pipeline:init"
	key := run.ID.String()
	parentBatchID := candidates[0].BatchID
	var parent *uuid.UUID
	if parentBatchID != uuid.Nil {
		parent = &parentBatchID
	}
	var idempotencyKey *string
	if parent != nil {
		idempotencyKey = &key
	}
	payload, err := json.Marshal(map[string]string{
		"pipeline_file":            defaultPipelineFile,
		"pipeline_definition_hash": definitionHash,
		"analysis_run_id":          run.ID.String(),
	})
	if err != nil {
		return repo.AnalysisRun{}, err
	}
	_, createErr := s.PipelineRuntime.CreatePipelineRoot(ctx, repo.CreateTaskParams{
		BatchID: rootID, ParentBatchID: parent, Kind: repo.TaskKindPipelineInit,
		AnalysisExecutionID: run.ExecutionID,
		SourceType:          repo.SourceTypeMedia, SourceAbbr: candidates[0].SourceAbbr,
		URL: "pipeline://analysis/" + run.ID.String(), Payload: payload, TraceID: traceID,
		LogicalKey: &logicalKey, PipelineDefinitionHash: definitionHash,
		PipelineIdempotencyKey: idempotencyKey, PipelineRequestFingerprint: requestFingerprint,
		PipelineInputCandidateIDs: run.ReadyCandidateIDs,
		PipelineInputContentIDs:   run.ReadyContentIDs,
	})
	if createErr != nil && !errors.Is(createErr, repo.ErrTaskAlreadyActive) {
		failure := stringPtr("ANALYSIS_ROOT_FAILED")
		_, _ = s.AnalysisRuns.SetStatus(ctx, repo.SetAnalysisRunStatusParams{
			ID: run.ID, Status: repo.AnalysisRunStatusFailed,
			FailedCandidateIDs: run.FailedCandidateIDs, FailureCode: failure,
		})
		return repo.AnalysisRun{}, createErr
	}
	return s.AnalysisRuns.SetRoot(ctx, run.ID, rootID)
}

func (s *Server) writeAnalysisTransitionError(w http.ResponseWriter, err error) {
	s.Logger.Error("analysis run transition failed", slog.Any("error", err))
	writeError(w, http.StatusInternalServerError, "failed to resolve analysis run")
}

func (s *Server) classifyCandidates(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, []uuid.UUID, error) {
	candidates, err := s.Scout.GetCandidatesByIDs(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[uuid.UUID]repo.Candidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.ID] = candidate
	}
	available := make([]uuid.UUID, 0, len(ids))
	unavailable := make([]uuid.UUID, 0)
	for _, id := range ids {
		candidate, found := byID[id]
		if found {
			if _, err := utils.NormalizeURL(candidate.URL); err == nil {
				available = append(available, id)
				continue
			}
		}
		unavailable = append(unavailable, id)
	}
	return available, unavailable, nil
}

func validateAnalysisIDs(ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, errors.New("ANALYSIS_INPUT_EMPTY")
	}
	if len(ids) > maxAnalysisCandidates {
		return nil, fmt.Errorf("too many candidate_ids (max %d)", maxAnalysisCandidates)
	}

	slices.SortFunc(ids, func(a, b uuid.UUID) int {
		return bytes.Compare(a[:], b[:])
	})

	unique := []uuid.UUID{ids[0]}
	for _, id := range ids[1:] {
		if id == uuid.Nil {
			return nil, errors.New("candidate_ids contains an invalid id")
		}
		if unique[len(unique)-1] != id {
			unique = append(unique, id)
		}
	}
	// Check the first element as well, since we skipped it in the loop
	if unique[0] == uuid.Nil {
		return nil, errors.New("candidate_ids contains an invalid id")
	}

	return unique, nil
}

func (s *Server) loadAnalysisRun(ctx context.Context, w http.ResponseWriter, rawID string) (repo.AnalysisRun, bool) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid analysis run id")
		return repo.AnalysisRun{}, false
	}
	run, err := s.AnalysisRuns.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "analysis run not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load analysis run")
		}
		return repo.AnalysisRun{}, false
	}
	principal, ok := middleware.PrincipalFromContext(ctx)
	if !ok || run.UserID == nil || *run.UserID != principal.TokenID {
		writeError(w, http.StatusNotFound, "analysis run not found")
		return repo.AnalysisRun{}, false
	}
	return run, true
}

func stringPtr(value string) *string { return &value }

func analysisRunResponse(run repo.AnalysisRun) AnalysisRunResponse {
	return AnalysisRunResponse{AnalysisID: run.ID, AnalysisRunID: run.ID, FetchID: run.FetchID, Status: run.Status}
}
