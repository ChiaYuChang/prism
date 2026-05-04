package appconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("empty path returns empty value, no error", func(t *testing.T) {
		v, err := LoadFromFile("")
		require.NoError(t, err)
		assert.Empty(t, v)
	})

	t.Run("trims trailing newline", func(t *testing.T) {
		path := filepath.Join(dir, "tok")
		require.NoError(t, os.WriteFile(path, []byte("secret-value\n"), 0o600))
		v, err := LoadFromFile(path)
		require.NoError(t, err)
		assert.Equal(t, "secret-value", v)
	})

	t.Run("trims surrounding whitespace including CRLF", func(t *testing.T) {
		path := filepath.Join(dir, "tok2")
		require.NoError(t, os.WriteFile(path, []byte("secret-value\r\n"), 0o600))
		v, err := LoadFromFile(path)
		require.NoError(t, err)
		assert.Equal(t, "secret-value", v)
	})

	t.Run("missing file returns wrapped error", func(t *testing.T) {
		_, err := LoadFromFile(filepath.Join(dir, "does-not-exist"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read secret file")
		assert.Contains(t, err.Error(), "does-not-exist")
	})

	t.Run("preserves internal whitespace", func(t *testing.T) {
		// Defends against an over-aggressive trim that would corrupt
		// base64-padded or token-with-internal-space values.
		path := filepath.Join(dir, "tok3")
		require.NoError(t, os.WriteFile(path, []byte("a b\tc\n"), 0o600))
		v, err := LoadFromFile(path)
		require.NoError(t, err)
		assert.Equal(t, "a b\tc", v)
	})
}
