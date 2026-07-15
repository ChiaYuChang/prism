package filesystem_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/ChiaYuChang/prism/internal/storage/filesystem"
	"github.com/stretchr/testify/require"
)

func TestLocalStoreRoundTripListAndDelete(t *testing.T) {
	store, err := filesystem.NewLocalStore(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, store.Put(ctx, "nested/value.txt", strings.NewReader("hello"), storage.PutOptions{ContentType: "text/plain"}))

	object, err := store.Get(ctx, "nested/value.txt")
	require.NoError(t, err)
	body, err := io.ReadAll(object)
	require.NoError(t, object.Close())
	require.NoError(t, err)
	require.Equal(t, "hello", string(body))

	objects, err := store.List(ctx, "nested/")
	require.NoError(t, err)
	require.Equal(t, []storage.Object{{Key: "nested/value.txt", Size: 5}}, objects)

	require.NoError(t, store.Delete(ctx, "nested/value.txt"))
	_, err = store.Get(ctx, "nested/value.txt")
	require.Error(t, err)
	require.True(t, errors.Is(err, storage.ErrNotFound))
}

func TestLocalStoreRejectsPathTraversal(t *testing.T) {
	store, err := filesystem.NewLocalStore(t.TempDir())
	require.NoError(t, err)

	_, err = store.Get(context.Background(), "../outside")
	require.Error(t, err)
}

func TestNewLocalStoreRequiresRoot(t *testing.T) {
	_, err := filesystem.NewLocalStore(" ")
	require.Error(t, err)
}
