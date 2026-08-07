package filesystem_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
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

func TestLocalStorePutIfAbsentMatchesByContent(t *testing.T) {
	store, err := filesystem.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	ctx := context.Background()
	body := "deterministic report"

	result, err := store.PutIfAbsent(ctx, "reports/result.md", strings.NewReader(body), storage.PutOptions{ContentType: "text/markdown"})
	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, int64(len(body)), result.Metadata.Size)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(body))), result.Metadata.SHA256)

	metadata, err := store.Stat(ctx, "reports/result.md")
	require.NoError(t, err)
	require.Equal(t, result.Metadata.Key, metadata.Key)
	require.Equal(t, result.Metadata.Size, metadata.Size)
	require.Equal(t, result.Metadata.SHA256, metadata.SHA256)

	result, err = store.PutIfAbsent(ctx, "reports/result.md", strings.NewReader(body), storage.PutOptions{})
	require.NoError(t, err)
	require.False(t, result.Created)
	require.Equal(t, metadata.SHA256, result.Metadata.SHA256)

	result, err = store.PutIfAbsent(ctx, "reports/result.md", strings.NewReader("different report"), storage.PutOptions{})
	require.ErrorIs(t, err, storage.ErrContentMismatch)
	require.False(t, result.Created)
	require.Equal(t, metadata.SHA256, result.Metadata.SHA256)

	object, err := store.Get(ctx, "reports/result.md")
	require.NoError(t, err)
	got, readErr := io.ReadAll(object)
	require.NoError(t, object.Close())
	require.NoError(t, readErr)
	require.Equal(t, body, string(got))
}

func TestLocalStorePutIfAbsentDoesNotOverwriteConcurrently(t *testing.T) {
	store, err := filesystem.NewLocalStore(t.TempDir())
	require.NoError(t, err)

	const writers = 8
	type outcome struct {
		result storage.PutIfAbsentResult
		err    error
	}
	outcomes := make(chan outcome, writers)
	var wait sync.WaitGroup
	for i := 0; i < writers; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			body := fmt.Sprintf("report from writer %d", i)
			result, err := store.PutIfAbsent(context.Background(), "reports/concurrent.md", strings.NewReader(body), storage.PutOptions{})
			outcomes <- outcome{result: result, err: err}
		}(i)
	}
	wait.Wait()
	close(outcomes)

	created := 0
	for got := range outcomes {
		if got.err == nil {
			if got.result.Created {
				created++
			}
			continue
		}
		require.ErrorIs(t, got.err, storage.ErrContentMismatch)
	}
	require.Equal(t, 1, created)

	metadata, err := store.Stat(context.Background(), "reports/concurrent.md")
	require.NoError(t, err)
	require.NotEmpty(t, metadata.SHA256)
}

func TestLocalStoreStatNotFoundAndPutIfAbsentBounded(t *testing.T) {
	store, err := filesystem.NewLocalStore(t.TempDir())
	require.NoError(t, err)

	_, err = store.Stat(context.Background(), "reports/missing.md")
	require.ErrorIs(t, err, storage.ErrNotFound)

	tooLarge := strings.Repeat("x", int(storage.MaxObjectSize)+1)
	_, err = store.PutIfAbsent(context.Background(), "reports/large.md", strings.NewReader(tooLarge), storage.PutOptions{})
	require.ErrorIs(t, err, storage.ErrObjectTooLarge)
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
