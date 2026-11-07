// Package web anchors the embedded SPA. The actual SPA build lands in M2;
// in M0 this file exists only so `go build` has a stable embed target and so
// the design's `//go:embed` pattern is in place from day one.
//
// Build order (design §19.1, §F10):
//   1. `make web` writes web/dist/ (Vite output)
//   2. `make build` compiles cmd/tailflow, which embeds dist/ via this file
//   3. internal/api.Handler serves embedded files via spaFS
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distFS embed.FS

// DistFS returns the embedded SPA filesystem rooted at dist/. The caller
// (cmd/tailflow) passes this to api.NewHandler as the SPA fallback source.
// In M0 dist/ contains a placeholder index.html so the embed directive
// resolves; the real Vite build replaces it in M2.
func DistFS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Should not happen: dist/ is always embedded by the directive above.
		panic("web: cannot subtree dist/: " + err.Error())
	}
	return sub
}
