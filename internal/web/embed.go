//go:build web

package web

import (
	"embed"
	"io/fs"
)

// webDist holds the compiled React SPA produced by `npm run build`.
// The Vite outDir is configured to write to internal/web/ui/dist so that
// go:embed can reference it with a simple relative path (no ../ allowed).
//
// Build this with: go build -tags web ./cmd/feino
//
//go:embed all:ui/dist
var webDist embed.FS

// EmbeddedFS returns a sub-filesystem rooted at the ui/dist directory.
// Callers receive an fs.FS without the leading "ui/dist/" path prefix.
func EmbeddedFS() fs.FS {
	sub, _ := fs.Sub(webDist, "ui/dist")
	return sub
}
