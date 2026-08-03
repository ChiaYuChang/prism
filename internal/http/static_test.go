package prismhttp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewSPAHandler(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "index.html"), []byte("control room"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("bundle"), 0o644))

	handler, err := NewSPAHandler(root)
	require.NoError(t, err)

	t.Run("client route falls back to index", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/overview", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "control room", rec.Body.String())
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	})

	t.Run("assets are cacheable", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "bundle", rec.Body.String())
		require.Equal(t, "public, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))
	})

	t.Run("missing asset is not replaced by index", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
		require.Equal(t, http.StatusNotFound, rec.Code)
	})
}
