//go:build webui

package frontend

import (
	"embed"
	"io/fs"
)

// all: includes SvelteKit's _app directory, which embed otherwise excludes.
//
//go:embed all:build
var assets embed.FS

func Assets() (fs.FS, error) {
	return fs.Sub(assets, "build")
}
