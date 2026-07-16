// Package webui serves the SvelteKit-built UI as static files.
//
// The embed directive pulls in `dist/` at compile time. The CP Dockerfile runs
// the UI build in an earlier stage and copies its output into this dist/
// directory before invoking `go build`. For local dev, set SPINUP_UI_STATIC_DIR
// to an absolute path (typically pointing at apps/ui/build) and Handler()
// falls through to the filesystem instead of the embedded FS.
package webui

import "embed"

//go:embed all:dist
var files embed.FS

// Files returns the embedded SvelteKit build (or a nearly-empty FS in local
// dev builds where the Dockerfile hasn't populated dist/).
func Files() embed.FS { return files }
