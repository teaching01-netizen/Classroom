package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newSpaFixture(t *testing.T) http.Handler {
	t.Helper()
	distDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<!doctype html>"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(distDir, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(distDir, "assets", "index-abc123.js"), []byte("console.log('hi')"), 0o644))
	return spaFallbackHandler(distDir)
}

func TestSpaFallbackHandler_ServesShellWithoutCaching(t *testing.T) {
	handler := newSpaFixture(t)

	for _, path := range []string{"/", "/courses/2211/sessions"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
			require.Contains(t, rec.Body.String(), "<!doctype html>")
		})
	}
}

func TestSpaFallbackHandler_CachesHashedAssetsAsImmutable(t *testing.T) {
	handler := newSpaFixture(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/index-abc123.js", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "public, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))
	require.Contains(t, rec.Header().Get("Content-Type"), "javascript")
}

func TestSpaFallbackHandler_MissingAssetReturns404NotSpaShell(t *testing.T) {
	handler := newSpaFixture(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/index-stale.js", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.NotContains(t, rec.Body.String(), "<!doctype html>")
}
