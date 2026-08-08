package pipeline

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
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

type immutableReportStore struct {
	body []byte
}

func (s *immutableReportStore) Put(context.Context, string, io.Reader, storage.PutOptions) error {
	return nil
}
func (s *immutableReportStore) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(s.body))), nil
}
func (s *immutableReportStore) List(context.Context, string) ([]storage.Object, error) {
	return nil, nil
}
func (s *immutableReportStore) Delete(context.Context, string) error { return nil }
func (s *immutableReportStore) PutIfAbsent(_ context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.PutIfAbsentResult, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return storage.PutIfAbsentResult{}, err
	}
	s.body = data
	digest := sha256.Sum256(data)
	return storage.PutIfAbsentResult{
		Created: true,
		Metadata: storage.ObjectMetadata{
			Key: key, Size: int64(len(data)), ContentType: opts.ContentType,
		},
		Checksum: storage.ObjectChecksum{
			SHA256: fmt.Sprintf("%x", digest),
			Size:   int64(len(data)),
		},
	}, nil
}
func (s *immutableReportStore) Stat(ctx context.Context, key string) (storage.ObjectMetadata, error) {
	return storage.ObjectMetadata{}, storage.ErrNotFound
}
func (s *immutableReportStore) Checksum(ctx context.Context, key string) (storage.ObjectChecksum, error) {
	digest := sha256.Sum256(s.body)
	return storage.ObjectChecksum{SHA256: fmt.Sprintf("%x", digest), Size: int64(len(s.body))}, nil
}

func TestMarkdownReportWriterDeliversDeterministicArtifact(t *testing.T) {
	store := &immutableReportStore{}
	runtime := mocks.NewMockPipelineRuntime(t)
	executionID := uuid.New()
	rootID := uuid.New()
	taskID := uuid.New()
	ttl := time.Hour
	writer, err := NewMarkdownReportWriter(store, runtime, ttl)
	require.NoError(t, err)
	runtime.EXPECT().CompleteAnalysisReport(mock.Anything, mock.MatchedBy(func(arg repo.CompleteAnalysisReportParams) bool {
		return arg.TaskID == taskID && arg.RootBatchID == rootID && arg.AnalysisExecutionID == executionID &&
			arg.Report.StorageURI == "reports/"+executionID.String()+".md" && arg.Report.ByteSize == int64(len(store.body)) &&
			arg.Report.SHA256 != "" && arg.Report.ExpiresAt.After(time.Now())
	})).Return(repo.Report{}, nil).Once()

	err = writer.Deliver(context.Background(), repo.Task{ID: taskID}, repo.Batch{
		ID: rootID, AnalysisExecutionID: &executionID,
	}, WorkSetInput{
		ContentIDs: []uuid.UUID{uuid.MustParse("00000000-0000-0000-0000-000000000002"), uuid.MustParse("00000000-0000-0000-0000-000000000001")},
		ContentSnapshots: map[uuid.UUID]repo.Content{
			uuid.MustParse("00000000-0000-0000-0000-000000000001"): {ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Title: "First", URL: "https://example.com/1"},
			uuid.MustParse("00000000-0000-0000-0000-000000000002"): {ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), Title: "Second", URL: "https://example.com/2"},
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(store.body), "- First ([source](https://example.com/1))")
	require.Less(t, strings.Index(string(store.body), "- First"), strings.Index(string(store.body), "- Second"))
}
