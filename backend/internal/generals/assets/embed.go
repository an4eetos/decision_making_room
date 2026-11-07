// Package assets embeds the roster so the binary ships knowing its own lenses.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed generals/*.md styles/*.md traps.yaml
var files embed.FS

func Generals() fs.FS { return mustSub("generals") }
func Styles() fs.FS   { return mustSub("styles") }

// Traps is the cognitive-trap catalogue the generals' kills refer to.
func Traps() []byte {
	data, err := files.ReadFile("traps.yaml")
	if err != nil {
		panic(err) // embedded at compile time
	}
	return data
}

func mustSub(dir string) fs.FS {
	sub, err := fs.Sub(files, dir)
	if err != nil {
		panic(err) // the embedded layout is fixed at compile time
	}
	return sub
}
