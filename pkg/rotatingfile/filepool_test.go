package rotatingfile

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilePoolNewValidation(t *testing.T) {
	// 1. Rejects empty path
	_, err := New(Config{Path: "", MaxSize: "10MiB", MaxFiles: 5})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrParamMissing)

	// 2. Rejects invalid MaxSize parsing
	_, err = New(Config{Path: "test.log", MaxSize: "invalid", MaxFiles: 5})
	require.Error(t, err)

	// 3. Rejects max-files < 1 when MaxSize > 0
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "app.log")
	_, err = New(Config{Path: logPath, MaxSize: "10MiB", MaxFiles: 0})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max-files must be >= 1")
}

func TestFilePoolWriteSlot0(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "app.log")

	pool, err := New(Config{Path: logPath, MaxSize: "10B", MaxFiles: 3})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	assert.Equal(t, 3, pool.CreatedSlots())

	n, err := pool.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)

	// Verify it wrote to slot 0
	content, err := os.ReadFile(logPath + ".0")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))

	// Verify the fixed ring slots are pre-created.
	for _, suffix := range []string{".0", ".1", ".2"} {
		_, err = os.Stat(logPath + suffix)
		require.NoError(t, err)
	}
}

func TestFilePoolRotationAndReuse(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "app.log")

	// MaxSize: 5B, MaxFiles: 3
	pool, err := New(Config{Path: logPath, MaxSize: "5B", MaxFiles: 3})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	// Write 1: 3 bytes to slot 0
	_, err = pool.Write([]byte("abc"))
	require.NoError(t, err)

	// Write 2: 3 bytes. This would make size 6, exceeding 5. Should rotate to slot 1.
	_, err = pool.Write([]byte("def"))
	require.NoError(t, err)

	// Verify slot 0 content is "abc" and slot 1 content is "def"
	content0, err := os.ReadFile(logPath + ".0")
	require.NoError(t, err)
	assert.Equal(t, "abc", string(content0))

	content1, err := os.ReadFile(logPath + ".1")
	require.NoError(t, err)
	assert.Equal(t, "def", string(content1))

	// Write 3: 4 bytes. Exceeds max size on slot 1, should rotate to slot 2.
	_, err = pool.Write([]byte("ghij"))
	require.NoError(t, err)

	content2, err := os.ReadFile(logPath + ".2")
	require.NoError(t, err)
	assert.Equal(t, "ghij", string(content2))

	// Write 4: 3 bytes. Exceeds max size on slot 2, should wrap and reuse slot 0.
	// Slot 0 should be truncated.
	_, err = pool.Write([]byte("klm"))
	require.NoError(t, err)

	content0Updated, err := os.ReadFile(logPath + ".0")
	require.NoError(t, err)
	assert.Equal(t, "klm", string(content0Updated)) // Truncated and overwritten

	// Verify slot 3 does not exist (MaxFiles is 3, slots are 0, 1, 2)
	_, err = os.Stat(logPath + ".3")
	assert.True(t, os.IsNotExist(err))
}

func TestFilePoolExtendsPool(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "app.log")

	pool, err := New(Config{Path: logPath, MaxSize: "10B", MaxFiles: 2})
	require.NoError(t, err)
	require.NoError(t, pool.Close())

	pool, err = New(Config{Path: logPath, MaxSize: "10B", MaxFiles: 4})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	assert.Equal(t, 2, pool.CreatedSlots())

	for _, suffix := range []string{".0", ".1", ".2", ".3"} {
		_, err = os.Stat(logPath + suffix)
		require.NoError(t, err)
	}
}

func TestFilePoolRejectsShrunkPoolWithExistingStaleSlots(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "app.log")

	pool, err := New(Config{Path: logPath, MaxSize: "2B", MaxFiles: 4})
	require.NoError(t, err)
	_, err = pool.Write([]byte("aa"))
	require.NoError(t, err)
	_, err = pool.Write([]byte("bb"))
	require.NoError(t, err)
	_, err = pool.Write([]byte("cc"))
	require.NoError(t, err)
	require.NoError(t, pool.Close())

	pool, err = New(Config{Path: logPath, MaxSize: "2B", MaxFiles: 2})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max-files shrink")
	require.Nil(t, pool)

	_, err = os.Stat(logPath + ".0")
	require.NoError(t, err)
	_, err = os.Stat(logPath + ".1")
	require.NoError(t, err)
	_, err = os.Stat(logPath + ".2")
	require.NoError(t, err)
	_, err = os.Stat(logPath + ".3")
	require.NoError(t, err)
}

func TestFilePoolRestartsAtNewestNonEmptySlot(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "app.log")
	require.NoError(t, os.WriteFile(logPath+".0", []byte("old"), 0o644))
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, os.WriteFile(logPath+".1", []byte("new"), 0o644))

	pool, err := New(Config{Path: logPath, MaxSize: "10B", MaxFiles: 3})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	_, err = pool.Write([]byte("er"))
	require.NoError(t, err)
	require.NoError(t, pool.Sync())

	assert.Equal(t, "old", readFile(t, logPath+".0"))
	assert.Equal(t, "newer", readFile(t, logPath+".1"))
}

func TestFilePoolRestartsByRotatingWhenNewestSlotIsFull(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "app.log")
	require.NoError(t, os.WriteFile(logPath+".0", []byte("aa"), 0o644))
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, os.WriteFile(logPath+".1", []byte("bb"), 0o644))

	pool, err := New(Config{Path: logPath, MaxSize: "2B", MaxFiles: 3})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	_, err = pool.Write([]byte("cc"))
	require.NoError(t, err)
	require.NoError(t, pool.Sync())

	assert.Equal(t, "bb", readFile(t, logPath+".1"))
	assert.Equal(t, "cc", readFile(t, logPath+".2"))
}

func TestFilePoolIgnoresEmptyNewSlotsWhenInferringNewest(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "app.log")
	require.NoError(t, os.WriteFile(logPath+".0", []byte("active"), 0o644))
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, os.WriteFile(logPath+".1", nil, 0o644))

	pool, err := New(Config{Path: logPath, MaxSize: "20B", MaxFiles: 3})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	_, err = pool.Write([]byte("-next"))
	require.NoError(t, err)
	require.NoError(t, pool.Sync())

	assert.Equal(t, "active-next", readFile(t, logPath+".0"))
}

func TestFilePoolLargeSingleWrite(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "app.log")

	// MaxSize is 5B, payload is 10B. It should write to slot 0 and succeed.
	pool, err := New(Config{Path: logPath, MaxSize: "5B", MaxFiles: 3})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	_, err = pool.Write([]byte("1234567890"))
	require.NoError(t, err)

	content0, err := os.ReadFile(logPath + ".0")
	require.NoError(t, err)
	assert.Equal(t, "1234567890", string(content0))

	// Write 2: 3 bytes. Since slot 0 is not empty and exceeds max size, it rotates to slot 1.
	_, err = pool.Write([]byte("abc"))
	require.NoError(t, err)

	content1, err := os.ReadFile(logPath + ".1")
	require.NoError(t, err)
	assert.Equal(t, "abc", string(content1))
}

func TestFilePoolSyncAndClose(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "app.log")

	pool, err := New(Config{Path: logPath, MaxSize: "10B", MaxFiles: 3})
	require.NoError(t, err)

	_, err = pool.Write([]byte("test"))
	require.NoError(t, err)

	require.NoError(t, pool.Sync())

	require.NoError(t, pool.Close())

	// Writing after close should fail
	_, err = pool.Write([]byte("more"))
	require.ErrorIs(t, err, os.ErrClosed)

	// Sync after close should fail
	err = pool.Sync()
	require.ErrorIs(t, err, os.ErrClosed)
}

func TestFilePoolConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "app.log")

	pool, err := New(Config{Path: logPath, MaxSize: "100B", MaxFiles: 5})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	var wg sync.WaitGroup
	numGoroutines := 10
	writesPerGoroutine := 20
	payload := []byte("a")

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				_, err := pool.Write(payload)
				if err != nil {
					t.Errorf("write error: %v", err)
				}
			}
		}()
	}

	wg.Wait()
	require.NoError(t, pool.Sync())

	wantBytes := numGoroutines * writesPerGoroutine * len(payload)
	gotBytes := 0
	for i := 0; i < 5; i++ {
		info, err := os.Stat(logPath + "." + string(rune('0'+i)))
		require.NoError(t, err)
		gotBytes += int(info.Size())
	}
	assert.Equal(t, wantBytes, gotBytes)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}
