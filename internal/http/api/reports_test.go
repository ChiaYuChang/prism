package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type reportReadStore struct {
	body      string
	getCalls  int
	deleteErr error
}

func (s *reportReadStore) Put(context.Context, string, io.Reader, storage.PutOptions) error {
	return nil
}

func (s *reportReadStore) Get(context.Context, string) (io.ReadCloser, error) {
	s.getCalls++
	if s.body == "" {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(s.body)), nil
}

func (s *reportReadStore) List(context.Context, string) ([]storage.Object, error) { return nil, nil }

func (s *reportReadStore) Delete(context.Context, string) error { return s.deleteErr }

func TestGetAnalysisReportVerifiesAndReturnsArtifact(t *testing.T) {
	srv, _ := newTestServer(t)
	runs := mocks.NewMockAnalysisRuns(t)
	reports := mocks.NewMockReports(t)
	srv.AnalysisRuns = runs
	srv.Reports = reports
	userID := uuid.New()
	analysisID := uuid.New()
	executionID := uuid.New()
	reportID := uuid.New()
	body := "# report\n"
	store := &reportReadStore{body: body}
	srv.ReportStore = store
	digest := sha256.Sum256([]byte(body))
	runs.EXPECT().GetByID(mock.Anything, analysisID).Return(repo.AnalysisRun{
		ID: analysisID, UserID: &userID, Status: repo.AnalysisRunStatusCompleted,
		ExecutionID: &executionID, ReportID: &reportID,
	}, nil).Once()
	reports.EXPECT().GetByID(mock.Anything, reportID).Return(repo.Report{
		ID: reportID, AnalysisExecutionID: executionID, StorageURI: "reports/" + executionID.String() + ".md",
		ByteSize: int64(len(body)), SHA256: hex.EncodeToString(digest[:]),
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil).Once()

	req := withUserPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+analysisID.String()+"/report", nil), userID)
	req.SetPathValue("id", analysisID.String())
	rec := httptest.NewRecorder()
	srv.GetAnalysisReport(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, body, rec.Body.String())
	require.Equal(t, 1, store.getCalls)
}

func TestGetAnalysisReportExpiredSkipsStorage(t *testing.T) {
	srv, _ := newTestServer(t)
	runs := mocks.NewMockAnalysisRuns(t)
	reports := mocks.NewMockReports(t)
	srv.AnalysisRuns = runs
	srv.Reports = reports
	userID := uuid.New()
	analysisID := uuid.New()
	executionID := uuid.New()
	reportID := uuid.New()
	store := &reportReadStore{body: "# report\n"}
	srv.ReportStore = store
	runs.EXPECT().GetByID(mock.Anything, analysisID).Return(repo.AnalysisRun{
		ID: analysisID, UserID: &userID, ExecutionID: &executionID, ReportID: &reportID,
	}, nil).Once()
	reports.EXPECT().GetByID(mock.Anything, reportID).Return(repo.Report{
		ID: reportID, AnalysisExecutionID: executionID, ExpiresAt: time.Now().Add(-time.Minute),
	}, nil).Once()

	req := withUserPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+analysisID.String()+"/report", nil), userID)
	req.SetPathValue("id", analysisID.String())
	rec := httptest.NewRecorder()
	srv.GetAnalysisReport(rec, req)

	require.Equal(t, http.StatusGone, rec.Code)
	require.Contains(t, rec.Body.String(), "ANALYSIS_RESULT_EXPIRED")
	require.Zero(t, store.getCalls)
}

func TestGetAnalysisReportMissingMarksArtifact(t *testing.T) {
	srv, _ := newTestServer(t)
	runs := mocks.NewMockAnalysisRuns(t)
	reports := mocks.NewMockReports(t)
	srv.AnalysisRuns = runs
	srv.Reports = reports
	userID := uuid.New()
	analysisID := uuid.New()
	executionID := uuid.New()
	reportID := uuid.New()
	store := &reportReadStore{}
	srv.ReportStore = store
	runs.EXPECT().GetByID(mock.Anything, analysisID).Return(repo.AnalysisRun{
		ID: analysisID, UserID: &userID, ExecutionID: &executionID, ReportID: &reportID,
	}, nil).Once()
	reports.EXPECT().GetByID(mock.Anything, reportID).Return(repo.Report{
		ID: reportID, AnalysisExecutionID: executionID, ExpiresAt: time.Now().Add(time.Hour),
	}, nil).Once()
	reports.EXPECT().MarkMissing(mock.Anything, reportID, mock.Anything).Return(true, nil).Once()

	req := withUserPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+analysisID.String()+"/report", nil), userID)
	req.SetPathValue("id", analysisID.String())
	rec := httptest.NewRecorder()
	srv.GetAnalysisReport(rec, req)

	require.Equal(t, http.StatusGone, rec.Code)
	require.Contains(t, rec.Body.String(), "ANALYSIS_RESULT_ARTIFACT_MISSING")
	require.Equal(t, 1, store.getCalls)
}

func TestGetAnalysisReportForeignOwnerReturnsNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	runs := mocks.NewMockAnalysisRuns(t)
	srv.AnalysisRuns = runs
	ownerID := uuid.New()
	callerID := uuid.New()
	analysisID := uuid.New()
	runs.EXPECT().GetByID(mock.Anything, analysisID).Return(repo.AnalysisRun{
		ID: analysisID, UserID: &ownerID,
	}, nil).Once()

	req := withUserPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/analyses/"+analysisID.String()+"/report", nil), callerID)
	req.SetPathValue("id", analysisID.String())
	rec := httptest.NewRecorder()
	srv.GetAnalysisReport(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	var reportErr struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&reportErr))
	require.Equal(t, "ANALYSIS_NOT_FOUND", reportErr.Code)
}
