package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// staticDirWith writes files into a temp dir and returns its path — the
// handler's local-dev filesystem mode reads from here so we can smoke-test
// without needing the Dockerfile to populate the embedded dist/.
func staticDirWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return dir
}

func do(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestHandlerServesRootIndex(t *testing.T) {
	dir := staticDirWith(t, map[string]string{
		"index.html": "<html>root</html>",
	})
	h := Handler(dir)

	rr := do(t, h, "/")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "<html>root</html>") {
		t.Fatalf("body: got %q", rr.Body.String())
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control: got %q, want no-cache", cc)
	}
}

func TestHandlerServesRealAsset(t *testing.T) {
	dir := staticDirWith(t, map[string]string{
		"index.html":              "root",
		"_app/immutable/app.js":   "console.log('hi')",
	})
	h := Handler(dir)

	rr := do(t, h, "/_app/immutable/app.js")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "console.log") {
		t.Fatalf("body: got %q", rr.Body.String())
	}
	if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control: expected immutable header, got %q", cc)
	}
}

func TestHandlerSPAFallbackForClientRoute(t *testing.T) {
	dir := staticDirWith(t, map[string]string{
		"index.html": "<html>root</html>",
	})
	h := Handler(dir)

	// SvelteKit client-side route — no file at that path, no extension.
	rr := do(t, h, "/applications/abc123")
	if rr.Code != http.StatusOK {
		t.Fatalf("SPA fallback status: got %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "<html>root</html>") {
		t.Fatalf("SPA fallback body: got %q", rr.Body.String())
	}
}

func TestHandlerMissingAssetIs404(t *testing.T) {
	dir := staticDirWith(t, map[string]string{
		"index.html": "<html>root</html>",
	})
	h := Handler(dir)

	// Asset-shaped path (has extension) that doesn't exist — should 404,
	// NOT fall back to index.html (that would mask real bugs).
	rr := do(t, h, "/_app/immutable/missing.js")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing asset: got %d, want 404", rr.Code)
	}
}
