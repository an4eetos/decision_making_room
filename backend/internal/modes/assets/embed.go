// Package assets embeds the conversation modes.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed modes/*.md
var files embed.FS

func Modes() fs.FS {
	sub, err := fs.Sub(files, "modes")
	if err != nil {
		panic(err) // the embedded layout is fixed at compile time
	}
	return sub
}
