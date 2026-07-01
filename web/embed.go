// Package web embeds the built frontend so the whole thing ships as one Go binary
// with no Node at runtime. `dist` comes from `npm run build`; a placeholder is
// committed so the module always compiles even before you've built the frontend.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist returns the built frontend filesystem rooted at dist/.
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
