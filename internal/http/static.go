package prismhttp

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const controlRoomContentSecurityPolicy = "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"

// NewSPAHandler serves a static single-page application from root and falls
// back to index.html for client-side routes.
func NewSPAHandler(root string) (http.Handler, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("static root is empty")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat static root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("static root is not a directory: %s", root)
	}

	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		setStaticSecurityHeaders(w)

		path := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
		if path == "." {
			path = ""
		}
		if path != "" && pathExists(root, path) {
			if strings.HasPrefix(path, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		if path != "" && (strings.HasPrefix(path, "assets/") || filepath.Ext(path) != "") {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-store")
		r.URL.Path = "/"
		files.ServeHTTP(w, r)
	}), nil
}

func pathExists(root, path string) bool {
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return false
	}
	_, err := fs.Stat(os.DirFS(root), filepath.ToSlash(clean))
	return err == nil
}

func setStaticSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", controlRoomContentSecurityPolicy)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}
