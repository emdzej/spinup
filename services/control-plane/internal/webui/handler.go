package webui

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// Handler returns an http.Handler that serves the built SvelteKit UI.
//
// staticDir, when non-empty, points at a filesystem path (e.g., apps/ui/build)
// — useful in local dev with `go run` where the embedded FS is empty. In
// production the CP image bakes the UI into the embedded FS.
//
// For any request that doesn't map to a real file, the handler falls back to
// index.html — SvelteKit uses client-side routing, so /applications/{id} etc.
// need to resolve to the SPA entry point.
//
// API and framework routes (`/api/`, `/healthz`, `/metrics`) are handled by
// the outer mux BEFORE reaching this handler.
func Handler(staticDir string) http.Handler {
	var root fs.FS
	if staticDir != "" {
		root = os.DirFS(staticDir)
	} else {
		sub, err := fs.Sub(files, "dist")
		if err != nil {
			// unreachable — the embed always yields at least an empty dist/.
			return http.NotFoundHandler()
		}
		root = sub
	}

	fileServer := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try the requested path first. If it doesn't exist AND has no file
		// extension (i.e., it's a route, not a missing asset), serve index.html.
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(root, p); err != nil {
			// SPA fallback only when there's no dot in the last path segment —
			// i.e., don't serve index.html for missing /_app/immutable/foo.js
			// (that's a real bug worth surfacing as 404).
			last := path.Base(p)
			if !strings.Contains(last, ".") {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			} else {
				http.NotFound(w, r)
				return
			}
		}

		// Aggressive caching for immutable assets, no caching for HTML shells.
		if strings.HasPrefix(r.URL.Path, "/_app/immutable/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else if strings.HasSuffix(r.URL.Path, ".html") || r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}
