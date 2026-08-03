package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ChiaYuChang/prism/internal/http/middleware"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	analysisReportContentType = "text/markdown; charset=utf-8"
	analysisReportFilename    = "report.md"
)

// AnalysisResponse is the metadata representation returned by GET /analyses.
type AnalysisResponse struct {
	AnalysisID         uuid.UUID  `json:"analysis_id"`
	Status             string     `json:"status"`
	RootBatchID        *uuid.UUID `json:"observed_root_batch_id,omitempty"`
	ExecutionID        *uuid.UUID `json:"execution_id,omitempty"`
	ReportID           *uuid.UUID `json:"report_id,omitempty"`
	FailureCode        *string    `json:"failure_code,omitempty"`
	ReportAvailability string     `json:"report_availability,omitempty"`
}

// ReportRemovalRequest is the required reason for an administrative removal.
type ReportRemovalRequest struct {
	Reason string `json:"reason"`
}

// GetAnalysis returns owner-scoped analysis metadata without contacting
// artifact storage.
func (s *Server) GetAnalysis(w http.ResponseWriter, r *http.Request) {
	run, ok := s.loadOwnedAnalysis(r, w)
	if !ok {
		return
	}
	availability := ""
	if run.ReportID == nil || s.Reports == nil {
		if run.Status == repo.AnalysisRunStatusCompleted {
			availability = string(repo.ReportAvailabilityCorrupt)
		} else {
			availability = "NOT_READY"
		}
	} else {
		report, err := s.Reports.GetByID(r.Context(), *run.ReportID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				availability = string(repo.ReportAvailabilityCorrupt)
			} else {
				s.Logger.ErrorContext(r.Context(), "load analysis report metadata failed", "error", err)
				writeError(w, http.StatusInternalServerError, "failed to load analysis report")
				return
			}
		} else {
			if run.ExecutionID == nil || report.AnalysisExecutionID != *run.ExecutionID {
				availability = string(repo.ReportAvailabilityCorrupt)
			} else {
				availability = reportAvailability(report, time.Now())
			}
		}
	}
	writeJSON(w, http.StatusOK, AnalysisResponse{
		AnalysisID:         run.ID,
		Status:             run.Status,
		RootBatchID:        run.RootBatchID,
		ExecutionID:        run.ExecutionID,
		ReportID:           run.ReportID,
		FailureCode:        run.FailureCode,
		ReportAvailability: availability,
	})
}

// GetAnalysisReport returns the verified Markdown artifact for an owned
// analysis request. It buffers the bounded object before writing headers.
func (s *Server) GetAnalysisReport(w http.ResponseWriter, r *http.Request) {
	run, ok := s.loadOwnedAnalysis(r, w)
	if !ok {
		return
	}
	if r.Header.Get("Range") != "" {
		writeReportError(w, http.StatusRequestedRangeNotSatisfiable, "ANALYSIS_REPORT_RANGE_UNSUPPORTED", "report ranges are not supported")
		return
	}
	if run.ReportID == nil || s.Reports == nil {
		writeReportError(w, http.StatusConflict, "ANALYSIS_REPORT_NOT_READY", "analysis report is not ready")
		return
	}
	report, err := s.Reports.GetByID(r.Context(), *run.ReportID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeReportError(w, http.StatusInternalServerError, "ANALYSIS_REPORT_CORRUPT", "analysis report link is missing")
			return
		}
		s.Logger.ErrorContext(r.Context(), "load analysis report failed", "error", err)
		writeReportError(w, http.StatusInternalServerError, "ANALYSIS_REPORT_CORRUPT", "analysis report metadata is invalid")
		return
	}
	if run.ExecutionID == nil || report.AnalysisExecutionID != *run.ExecutionID {
		writeReportError(w, http.StatusNotFound, "ANALYSIS_REPORT_NOT_FOUND", "analysis report not found")
		return
	}

	now := time.Now()
	if report.ArtifactRemovedAt != nil {
		writeReportError(w, http.StatusGone, "ANALYSIS_RESULT_REMOVED_BY_ADMIN", "analysis report was removed by an administrator")
		return
	}
	if !now.Before(report.ExpiresAt) {
		writeReportError(w, http.StatusGone, "ANALYSIS_RESULT_EXPIRED", "analysis report has expired")
		return
	}
	if report.ArtifactMissingAt != nil {
		writeReportError(w, http.StatusGone, "ANALYSIS_RESULT_ARTIFACT_MISSING", "analysis report artifact is missing")
		return
	}
	if report.ArtifactCorruptAt != nil {
		writeReportError(w, http.StatusInternalServerError, "ANALYSIS_REPORT_CORRUPT", "analysis report artifact is corrupt")
		return
	}
	if s.ReportStore == nil {
		writeReportError(w, http.StatusServiceUnavailable, "ANALYSIS_REPORT_UNAVAILABLE", "analysis report storage is unavailable")
		return
	}

	body, err := s.readAndVerifyReport(r, report)
	if err != nil {
		s.handleReportReadError(w, r, report, err)
		return
	}
	if report.ArtifactRemovedAt != nil || !time.Now().Before(report.ExpiresAt) {
		writeReportError(w, http.StatusGone, "ANALYSIS_RESULT_EXPIRED", "analysis report is no longer available")
		return
	}
	w.Header().Set("Content-Type", analysisReportContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.Header().Set("Content-Disposition", `inline; filename="report.md"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// DeleteAdminReport removes an artifact after recording an operator decision.
func (s *Server) DeleteAdminReport(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		writeReportError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication is required")
		return
	}
	executionID, err := uuid.Parse(r.PathValue("execution_id"))
	if err != nil {
		writeReportError(w, http.StatusBadRequest, "ANALYSIS_REPORT_ID_INVALID", "invalid execution id")
		return
	}
	var req ReportRemovalRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || strings.TrimSpace(req.Reason) == "" || len([]byte(req.Reason)) > 1024 {
		writeReportError(w, http.StatusBadRequest, "ANALYSIS_REPORT_REMOVAL_REASON_INVALID", "a non-empty removal reason of at most 1024 bytes is required")
		return
	}
	if s.Reports == nil || s.ReportStore == nil {
		writeReportError(w, http.StatusServiceUnavailable, "ANALYSIS_REPORT_REMOVAL_PENDING", "report storage is unavailable")
		return
	}
	requestID := middleware.RequestIDFromContext(r.Context())
	var requestIDPtr *string
	if requestID != "" {
		requestIDPtr = &requestID
	}
	actorName := strings.TrimSpace(principal.Name)
	var actorNamePtr *string
	if actorName != "" {
		actorNamePtr = &actorName
	}
	report, err := s.Reports.BeginRemoval(r.Context(), repo.BeginReportRemovalParams{
		AnalysisExecutionID: executionID,
		ActorTokenID:        principal.TokenID,
		ActorName:           actorNamePtr,
		Reason:              strings.TrimSpace(req.Reason),
		RequestID:           requestIDPtr,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeReportError(w, http.StatusNotFound, "ANALYSIS_REPORT_NOT_FOUND", "analysis report not found")
			return
		}
		s.Logger.ErrorContext(r.Context(), "mark report removed failed", "error", err)
		writeReportError(w, http.StatusInternalServerError, "ANALYSIS_REPORT_REMOVAL_FAILED", "failed to record report removal")
		return
	}
	deleteErr := s.ReportStore.Delete(r.Context(), report.StorageURI)
	outcome := "DELETED"
	var storageErr *string
	if deleteErr != nil {
		if errors.Is(deleteErr, storage.ErrNotFound) {
			outcome = "NOT_FOUND"
		} else {
			outcome = "FAILED"
			message := deleteErr.Error()
			storageErr = &message
		}
	}
	if auditErr := s.Reports.RecordAuditEvent(r.Context(), repo.ReportAuditEventParams{
		ReportID:            report.ID,
		AnalysisExecutionID: report.AnalysisExecutionID,
		EventType:           "ADMIN_REMOVE_ATTEMPT",
		ActorTokenID:        &principal.TokenID,
		ActorComponent:      "operator",
		ActorName:           actorNamePtr,
		Reason:              &req.Reason,
		RequestID:           requestIDPtr,
		StorageURI:          report.StorageURI,
		StorageOutcome:      outcome,
		StorageError:        storageErr,
	}); auditErr != nil {
		s.Logger.ErrorContext(r.Context(), "record report removal attempt failed", "error", auditErr)
	}
	if deleteErr != nil && !errors.Is(deleteErr, storage.ErrNotFound) {
		writeReportError(w, http.StatusServiceUnavailable, "ANALYSIS_REPORT_REMOVAL_PENDING", "report removal is pending")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) loadOwnedAnalysis(r *http.Request, w http.ResponseWriter) (repo.AnalysisRun, bool) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		writeReportError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication is required")
		return repo.AnalysisRun{}, false
	}
	if s.AnalysisRuns == nil {
		writeReportError(w, http.StatusServiceUnavailable, "ANALYSIS_UNAVAILABLE", "analysis service is unavailable")
		return repo.AnalysisRun{}, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeReportError(w, http.StatusBadRequest, "ANALYSIS_ID_INVALID", "invalid analysis id")
		return repo.AnalysisRun{}, false
	}
	run, err := s.AnalysisRuns.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeReportError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND", "analysis not found")
		} else {
			s.Logger.ErrorContext(r.Context(), "load analysis failed", "error", err)
			writeReportError(w, http.StatusInternalServerError, "ANALYSIS_UNAVAILABLE", "failed to load analysis")
		}
		return repo.AnalysisRun{}, false
	}
	if run.UserID == nil || *run.UserID != principal.TokenID {
		writeReportError(w, http.StatusNotFound, "ANALYSIS_NOT_FOUND", "analysis not found")
		return repo.AnalysisRun{}, false
	}
	return run, true
}

func (s *Server) readAndVerifyReport(r *http.Request, report repo.Report) ([]byte, error) {
	object, err := s.ReportStore.Get(r.Context(), report.StorageURI)
	if err != nil {
		return nil, err
	}
	defer func() { _ = object.Close() }()
	body, err := io.ReadAll(io.LimitReader(object, storage.MaxObjectSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > storage.MaxObjectSize || int64(len(body)) != report.ByteSize {
		return nil, storage.ErrContentMismatch
	}
	digest := sha256.Sum256(body)
	if !bytes.Equal(digest[:], mustDecodeHash(report.SHA256)) {
		return nil, storage.ErrContentMismatch
	}
	return body, nil
}

func (s *Server) handleReportReadError(w http.ResponseWriter, r *http.Request, report repo.Report, err error) {
	if errors.Is(err, storage.ErrNotFound) {
		_, markErr := s.Reports.MarkMissing(r.Context(), report.ID, requestIDPointer(r.Context()))
		if markErr != nil {
			s.Logger.ErrorContext(r.Context(), "mark missing report failed", "error", markErr)
		}
		writeReportError(w, http.StatusGone, "ANALYSIS_RESULT_ARTIFACT_MISSING", "analysis report artifact is missing")
		return
	}
	if errors.Is(err, storage.ErrContentMismatch) || errors.Is(err, storage.ErrObjectTooLarge) {
		_, markErr := s.Reports.MarkCorrupt(r.Context(), repo.MarkReportCorruptParams{
			ReportID:  report.ID,
			Reason:    "stored report bytes do not match the persisted size or SHA-256",
			RequestID: requestIDPointer(r.Context()),
		})
		if markErr != nil {
			s.Logger.ErrorContext(r.Context(), "mark corrupt report failed", "error", markErr)
		}
		writeReportError(w, http.StatusInternalServerError, "ANALYSIS_REPORT_CORRUPT", "analysis report artifact is corrupt")
		return
	}
	writeReportError(w, http.StatusServiceUnavailable, "ANALYSIS_REPORT_UNAVAILABLE", "analysis report storage is unavailable")
}

func reportAvailability(report repo.Report, now time.Time) string {
	if report.ArtifactRemovedAt != nil {
		return string(repo.ReportAvailabilityRemoved)
	}
	if !now.Before(report.ExpiresAt) {
		return string(repo.ReportAvailabilityExpired)
	}
	if report.ArtifactMissingAt != nil {
		return string(repo.ReportAvailabilityMissing)
	}
	if report.ArtifactCorruptAt != nil {
		return string(repo.ReportAvailabilityCorrupt)
	}
	return string(repo.ReportAvailabilityAvailable)
}

func writeReportError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message})
}

func requestIDPointer(ctx context.Context) *string {
	requestID := middleware.RequestIDFromContext(ctx)
	if requestID == "" {
		return nil
	}
	return &requestID
}

func mustDecodeHash(value string) []byte {
	digest, err := hex.DecodeString(value)
	if err != nil {
		return nil
	}
	return digest
}
