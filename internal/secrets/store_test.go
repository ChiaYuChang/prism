package secrets

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewKeyringStoreRequiresService(t *testing.T) {
	_, err := NewKeyringStore(" ")

	require.ErrorIs(t, err, ErrParamMissing)
}

func TestKeyringStoreGet(t *testing.T) {
	store := &KeyringStore{
		service: "prism",
		get: func(service, name string) (string, error) {
			assert.Equal(t, "prism", service)
			assert.Equal(t, "gemini-api-key", name)
			return "secret", nil
		},
	}

	value, err := store.Get(context.Background(), "gemini-api-key")

	require.NoError(t, err)
	assert.Equal(t, []byte("secret"), value)
}

func TestRBWStoreGet(t *testing.T) {
	store := &RBWStore{
		command: func(ctx context.Context, args ...string) ([]byte, error) {
			assert.Equal(t, []string{"get", "--raw", "gemini-api-key"}, args)
			return []byte(`{"password":"secret"}`), nil
		},
	}

	value, err := store.Get(context.Background(), "gemini-api-key")

	require.NoError(t, err)
	assert.Equal(t, []byte("secret"), value)
}

func TestRBWStoreGetNestedLoginPassword(t *testing.T) {
	store := &RBWStore{
		command: func(context.Context, ...string) ([]byte, error) {
			return []byte(`{"login":{"password":"secret"}}`), nil
		},
	}

	value, err := store.Get(context.Background(), "gemini-api-key")

	require.NoError(t, err)
	assert.Equal(t, []byte("secret"), value)
}

func TestRBWStoreGetDataPassword(t *testing.T) {
	store := &RBWStore{
		command: func(context.Context, ...string) ([]byte, error) {
			return []byte(`{"data":{"password":"secret"}}`), nil
		},
	}

	value, err := store.Get(context.Background(), "gemini-api-key")

	require.NoError(t, err)
	assert.Equal(t, []byte("secret"), value)
}

func TestRBWStorePropagatesCommandError(t *testing.T) {
	wantErr := errors.New("locked")
	store := &RBWStore{command: func(context.Context, ...string) ([]byte, error) {
		return nil, wantErr
	}}

	_, err := store.Get(context.Background(), "gemini-api-key")

	require.ErrorIs(t, err, wantErr)
}

func TestRBWStoreIntegration(t *testing.T) {
	if _, err := exec.LookPath("rbw"); err != nil {
		t.Skip("rbw is not installed")
	}
	if err := exec.Command("rbw", "unlocked").Run(); err != nil {
		t.Skip("rbw is not unlocked")
	}
	name := os.Getenv("PRISM_RBW_TEST_NAME")
	if name == "" {
		t.Skip("set PRISM_RBW_TEST_NAME to run against an unlocked rbw vault")
	}

	value, err := NewRBWStore().Get(context.Background(), name)

	require.NoError(t, err)
	assert.NotEmpty(t, value)
}

func TestKeyringStoreIntegration(t *testing.T) {
	name := os.Getenv("PRISM_KEYRING_TEST_NAME")
	if name == "" {
		t.Skip("set PRISM_KEYRING_TEST_NAME to run against an OS keyring item")
	}
	service := os.Getenv("PRISM_KEYRING_TEST_SERVICE")
	if service == "" {
		service = "prism"
	}

	store, err := NewKeyringStore(service)
	require.NoError(t, err)
	value, err := store.Get(context.Background(), name)

	require.NoError(t, err)
	assert.NotEmpty(t, value)
}

func TestSyncWritesMappedSecrets(t *testing.T) {
	dir := t.TempDir()
	store := fakeStore{values: map[string][]byte{"one": []byte("value")}}

	err := Sync(context.Background(), store, dir, []Mapping{{Item: "one", Path: "nested/secret"}})

	require.NoError(t, err)
	value, err := os.ReadFile(filepath.Join(dir, "nested", "secret"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), value)
	info, err := os.Stat(filepath.Join(dir, "nested", "secret"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o444), info.Mode().Perm())
}

func TestSyncRetrievesAllBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	store := fakeStore{values: map[string][]byte{"one": []byte("value")}, err: errors.New("missing")}

	err := Sync(context.Background(), store, dir, []Mapping{{Item: "one", Path: "one"}, {Item: "two", Path: "two"}})

	require.ErrorIs(t, err, store.err)
	_, statErr := os.Stat(filepath.Join(dir, "one"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

type fakeStore struct {
	values map[string][]byte
	err    error
}

func (s fakeStore) Get(_ context.Context, name string) ([]byte, error) {
	if value, ok := s.values[name]; ok {
		return value, nil
	}
	return nil, s.err
}

func TestStoresValidateContextAndName(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := NewRBWStore()

	_, err := store.Get(ctx, "secret")
	assert.ErrorIs(t, err, context.Canceled)

	_, err = store.Get(context.Background(), " ")
	assert.ErrorIs(t, err, ErrParamMissing)
}
