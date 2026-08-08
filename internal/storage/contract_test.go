package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/storage"
	"github.com/ChiaYuChang/prism/internal/storage/filesystem"
	"github.com/ChiaYuChang/prism/internal/storage/objectstore"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
)

func runStoreContract(t *testing.T, newStore func(t *testing.T) storage.Store) {
	ctx := context.Background()

	t.Run("Get missing", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Get(ctx, "missing.txt")
		require.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("Stat missing", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Stat(ctx, "missing.txt")
		require.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("Delete missing is idempotent", func(t *testing.T) {
		s := newStore(t)
		err := s.Delete(ctx, "missing.txt")
		require.NoError(t, err)

		err = s.Delete(ctx, "missing.txt")
		require.NoError(t, err)
	})

	t.Run("Put and Get round trip with overwrite", func(t *testing.T) {
		s := newStore(t)
		err := s.Put(ctx, "file.txt", bytes.NewReader([]byte("content")), storage.PutOptions{})
		require.NoError(t, err)

		rc, err := s.Get(ctx, "file.txt")
		require.NoError(t, err)
		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		require.Equal(t, "content", string(data))

		// Stat existing
		meta, err := s.Stat(ctx, "file.txt")
		require.NoError(t, err)
		require.Equal(t, int64(7), meta.Size)
		require.Equal(t, "file.txt", meta.Key)

		// Overwrite
		err = s.Put(ctx, "file.txt", bytes.NewReader([]byte("new content")), storage.PutOptions{})
		require.NoError(t, err)

		rc2, err := s.Get(ctx, "file.txt")
		require.NoError(t, err)
		data2, err := io.ReadAll(rc2)
		require.NoError(t, err)
		require.NoError(t, rc2.Close())
		require.Equal(t, "new content", string(data2))

		// Delete existing
		err = s.Delete(ctx, "file.txt")
		require.NoError(t, err)

		_, err = s.Get(ctx, "file.txt")
		require.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("List prefix semantics", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.Put(ctx, "archives/1.txt", bytes.NewReader(nil), storage.PutOptions{}))
		require.NoError(t, s.Put(ctx, "archives/2.txt", bytes.NewReader(nil), storage.PutOptions{}))
		require.NoError(t, s.Put(ctx, "archives2/3.txt", bytes.NewReader(nil), storage.PutOptions{}))
		
		objs, err := s.List(ctx, "archives/")
		require.NoError(t, err)
		
		var keys []string
		for _, o := range objs {
			keys = append(keys, o.Key)
		}
		sort.Strings(keys)
		require.Equal(t, []string{"archives/1.txt", "archives/2.txt"}, keys)
	})

	t.Run("Invalid keys", func(t *testing.T) {
		s := newStore(t)
		err := s.Put(ctx, "../escape", bytes.NewReader(nil), storage.PutOptions{})
		require.Error(t, err)
		require.NotErrorIs(t, err, storage.ErrProvider) // Must be validation error

		err = s.Put(ctx, "/absolute", bytes.NewReader(nil), storage.PutOptions{})
		require.Error(t, err)
		require.NotErrorIs(t, err, storage.ErrProvider)
	})
}

func runImmutableStoreContract(t *testing.T, newStore func(t *testing.T) storage.ImmutableStore) {
	runStoreContract(t, func(t *testing.T) storage.Store { return newStore(t) })

	ctx := context.Background()

	t.Run("PutIfAbsent semantics", func(t *testing.T) {
		s := newStore(t)
		content := []byte("immutable content")
		digest := fmt.Sprintf("%x", sha256.Sum256(content))

		res1, err := s.PutIfAbsent(ctx, "imm.txt", bytes.NewReader(content), storage.PutOptions{})
		require.NoError(t, err)
		require.True(t, res1.Created)
		require.Equal(t, digest, res1.Checksum.SHA256)
		require.Equal(t, int64(len(content)), res1.Checksum.Size)
		require.Equal(t, int64(len(content)), res1.Metadata.Size)

		// Same content
		res2, err := s.PutIfAbsent(ctx, "imm.txt", bytes.NewReader(content), storage.PutOptions{})
		require.NoError(t, err)
		require.False(t, res2.Created)
		require.Equal(t, digest, res2.Checksum.SHA256)

		// Different content
		_, err = s.PutIfAbsent(ctx, "imm.txt", bytes.NewReader([]byte("other")), storage.PutOptions{})
		require.ErrorIs(t, err, storage.ErrContentMismatch)

		// Checksum existing
		chk, err := s.Checksum(ctx, "imm.txt")
		require.NoError(t, err)
		require.Equal(t, digest, chk.SHA256)
		require.Equal(t, int64(len(content)), chk.Size)
	})

	t.Run("Checksum missing", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Checksum(ctx, "missing_checksum.txt")
		require.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("MaxObjectSize boundary", func(t *testing.T) {
		s := newStore(t)

		maxObj := make([]byte, storage.MaxObjectSize)
		_, err := s.PutIfAbsent(ctx, "max.txt", bytes.NewReader(maxObj), storage.PutOptions{})
		require.NoError(t, err)

		tooLarge := make([]byte, storage.MaxObjectSize+1)
		_, err = s.PutIfAbsent(ctx, "too_large.txt", bytes.NewReader(tooLarge), storage.PutOptions{})
		require.ErrorIs(t, err, storage.ErrObjectTooLarge)

		_, err = s.Checksum(ctx, "max.txt")
		require.NoError(t, err)

		// Put an oversized object via regular Put (which doesn't enforce the read-into-memory bounds of PutIfAbsent)
		err = s.Put(ctx, "too_large_chk.txt", bytes.NewReader(tooLarge), storage.PutOptions{})
		require.NoError(t, err)

		_, err = s.Checksum(ctx, "too_large_chk.txt")
		require.ErrorIs(t, err, storage.ErrObjectTooLarge)
	})
}

func TestLocalStore_Contract(t *testing.T) {
	runImmutableStoreContract(t, func(t *testing.T) storage.ImmutableStore {
		root := t.TempDir()
		store, err := filesystem.NewLocalStore(root)
		if err != nil {
			t.Fatalf("failed to create local store: %v", err)
		}
		return store
	})
}

func TestS3Store_Contract(t *testing.T) {
	if os.Getenv("PRISM_TEST_S3") == "" {
		t.Skip("set PRISM_TEST_S3=1 to run S3 integration tests (requires SeaweedFS/MinIO)")
	}

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("any", "any", "")),
	)
	require.NoError(t, err)

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String("http://localhost:8333") // SeaweedFS default S3 port
	})

	bucket := fmt.Sprintf("test-bucket-%d", time.Now().UnixNano())
	err = objectstore.EnsureBucket(context.Background(), client, bucket)
	require.NoError(t, err)

	runImmutableStoreContract(t, func(t *testing.T) storage.ImmutableStore {
		store, err := objectstore.NewS3Store(client, bucket, fmt.Sprintf("test-prefix-%d", time.Now().UnixNano()))
		require.NoError(t, err)
		return store
	})
}
