package prompt_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ChiaYuChang/prism/internal/prompt"
	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeMetadataGetter struct {
	ID   uuid.UUID
	Hash string
	Err  error
}

func (f *fakeMetadataGetter) GetByID(ctx context.Context, id uuid.UUID) (prompt.Version, error) {
	if f.Err != nil {
		return prompt.Version{}, f.Err
	}
	// Return the hardcoded ID if set, otherwise fallback to the requested ID
	returnedID := f.ID
	if returnedID == uuid.Nil {
		returnedID = id
	}

	return prompt.Version{
		ID:      returnedID,
		Hash:    f.Hash,
		Name:    "test",
		Version: 1,
	}, nil
}

type fakeGetter struct {
	Hash string
	Body string
}

func (f *fakeGetter) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	expectedKey := prompt.ObjectKey(f.Hash)
	if key == expectedKey {
		return io.NopCloser(strings.NewReader(f.Body)), nil
	}
	return nil, storage.ErrNotFound
}

func TestLoader_Success(t *testing.T) {
	body := "this is a test prompt"
	hashBytes := sha256.Sum256([]byte(body))
	hashStr := "sha256:" + hex.EncodeToString(hashBytes[:])
	id := uuid.New()

	metaGetter := &fakeMetadataGetter{
		ID:   id,
		Hash: hashStr,
	}

	getter := &fakeGetter{
		Hash: hashStr,
		Body: body,
	}

	verify := true
	resolver, err := prompt.NewResolver(getter, &verify)
	require.NoError(t, err)

	loader, err := prompt.NewLoader(metaGetter, resolver)
	require.NoError(t, err)

	loaded, err := loader.Load(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, id, loaded.ID)
	require.Equal(t, hashStr, loaded.Hash)
	require.Equal(t, []byte(body), loaded.Body)
}

func TestLoader_MetadataIDMismatch(t *testing.T) {
	requestedID := uuid.New()
	returnedID := uuid.New()

	metaGetter := &fakeMetadataGetter{
		ID:   returnedID, // mismatch
		Hash: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}

	resolver, _ := prompt.NewResolver(&fakeGetter{}, nil)
	loader, err := prompt.NewLoader(metaGetter, resolver)
	require.NoError(t, err)

	_, err = loader.Load(context.Background(), requestedID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt metadata id mismatch")
}

func TestLoader_MetadataError(t *testing.T) {
	id := uuid.New()
	metaGetter := &fakeMetadataGetter{
		Err: errors.New("db error"),
	}

	resolver, _ := prompt.NewResolver(&fakeGetter{}, nil)
	loader, err := prompt.NewLoader(metaGetter, resolver)
	require.NoError(t, err)

	_, err = loader.Load(context.Background(), id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "db error")
}

func TestLoader_ResolveError(t *testing.T) {
	id := uuid.New()
	metaGetter := &fakeMetadataGetter{
		ID:   id,
		Hash: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}

	resolver, _ := prompt.NewResolver(&fakeGetter{}, nil)
	loader, err := prompt.NewLoader(metaGetter, resolver)
	require.NoError(t, err)

	_, err = loader.Load(context.Background(), id)
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)
}

func TestLoader_NewLoaderValidation(t *testing.T) {
	resolver, _ := prompt.NewResolver(&fakeGetter{}, nil)
	metaGetter := &fakeMetadataGetter{}

	_, err := prompt.NewLoader(nil, resolver)
	require.Error(t, err)
	require.Contains(t, err.Error(), "metadata getter is required")

	_, err = prompt.NewLoader(metaGetter, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "resolver is required")
}
