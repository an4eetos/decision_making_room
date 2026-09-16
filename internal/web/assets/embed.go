// Package assets embeds the templates and static files so the server ships as a
// single binary. WEB_ROOT still overrides it from disk for anyone hacking on the
// frontend without rebuilding.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Templates returns the embedded templates directory rooted at its contents.
func Templates() fs.FS {
	sub, err := fs.Sub(templatesFS, "templates")
	if err != nil {
		panic(err) // embedded layout is fixed at compile time
	}
	return sub
}

// Static returns the embedded static directory rooted at its contents.
func Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
