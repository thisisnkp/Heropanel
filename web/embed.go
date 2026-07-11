// Package web embeds the built React SPA so hpd can serve the UI from a single
// binary (docs/08 §3). The real assets are produced by `npm run build` into
// web/dist; a .gitkeep placeholder keeps the directory present so this embed
// always compiles, even before a frontend build.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the SPA filesystem rooted at dist and whether a real build is
// present (index.html exists). When false, callers should serve a placeholder.
func FS() (fs.FS, bool) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return sub, false
	}
	return sub, true
}
