package prompt_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"testing"

	"github.com/ChiaYuChang/prism/internal/prompt"
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/internal/repo/mocks"
	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type testStore struct {
	body []byte
}

func (s testStore) Put(context.Context, string, io.Reader, storage.PutOptions) error { return nil }

func (s testStore) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.body)), nil
}

func (s testStore) List(context.Context, string) ([]storage.Object, error) { return nil, nil }

func (s testStore) Stat(context.Context, string) (storage.ObjectMetadata, error) {
	return storage.ObjectMetadata{}, storage.ErrNotFound
}

func (s testStore) Delete(context.Context, string) error { return nil }

func TestResolveUsesExactVersion(t *testing.T) {
	ctx := context.Background()
	body := []byte("version two")
	version := promptVersion("worker.planner.analysis.extractor", 2, body)
	prompts := mocks.NewMockPrompts(t)
	prompts.EXPECT().GetPromptVersionByNameAndVersion(ctx, "worker.planner.analysis.extractor", int32(2)).Return(version, nil)

	got, gotVersion, err := prompt.Resolve(ctx, prompts, testStore{body: body}, prompt.Ref{
		Key:     "worker/planner/analysis/extractor",
		Version: 2,
		Hash:    version.Hash,
	})
	require.NoError(t, err)
	require.Equal(t, body, got)
	require.Equal(t, version, gotVersion)
}

func TestResolveOnlyKeyUsesLatest(t *testing.T) {
	ctx := context.Background()
	body := []byte("latest")
	version := promptVersion("worker.planner.analysis.extractor", 3, body)
	prompts := mocks.NewMockPrompts(t)
	prompts.EXPECT().GetLatestPromptVersionByName(ctx, "worker.planner.analysis.extractor").Return(version, nil)

	got, _, err := prompt.Resolve(ctx, prompts, testStore{body: body}, prompt.Ref{Key: "worker/planner/analysis/extractor"})
	require.NoError(t, err)
	require.Equal(t, body, got)
}

func TestResolveRejectsIncompleteSelectors(t *testing.T) {
	ctx := context.Background()
	store := testStore{body: []byte("unused")}
	for name, ref := range map[string]prompt.Ref{
		"hash without version": {Key: "worker/planner/analysis/extractor", Hash: "sha256:expected"},
		"version without key":  {Version: 2},
		"hash without key":     {Hash: "sha256:expected"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := prompt.Resolve(ctx, mocks.NewMockPrompts(t), store, ref)
			require.Error(t, err)
		})
	}
}

func TestResolveRejectsExpectedHashMismatch(t *testing.T) {
	ctx := context.Background()
	body := []byte("version two")
	version := promptVersion("worker.planner.analysis.extractor", 2, body)
	prompts := mocks.NewMockPrompts(t)
	prompts.EXPECT().GetPromptVersionByNameAndVersion(ctx, "worker.planner.analysis.extractor", int32(2)).Return(version, nil)

	_, _, err := prompt.Resolve(ctx, prompts, testStore{body: body}, prompt.Ref{
		Key:     "worker/planner/analysis/extractor",
		Version: 2,
		Hash:    "sha256:wrong",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt hash mismatch")
}

func TestResolveRejectsObjectHashMismatch(t *testing.T) {
	ctx := context.Background()
	body := []byte("actual body")
	version := promptVersion("worker.planner.analysis.extractor", 2, []byte("stored body"))
	prompts := mocks.NewMockPrompts(t)
	prompts.EXPECT().GetPromptVersionByNameAndVersion(ctx, "worker.planner.analysis.extractor", int32(2)).Return(version, nil)

	_, _, err := prompt.Resolve(ctx, prompts, testStore{body: body}, prompt.Ref{
		Key:     "worker/planner/analysis/extractor",
		Version: 2,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt object hash mismatch")
}

func promptVersion(name string, version int32, body []byte) repo.PromptVersion {
	sum := sha256.Sum256(body)
	return repo.PromptVersion{
		ID:        uuid.New(),
		Name:      name,
		Version:   version,
		Hash:      fmt.Sprintf("sha256:%x", sum[:]),
		SizeBytes: int64(len(body)),
	}
}
