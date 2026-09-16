// Package assets embeds the relocation knowledge base. It ships in the binary so
// a fresh install knows what a three-month stay needs, and can be extended from
// disk without a rebuild.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed catalog/*.yaml pitfalls/*.yaml
var files embed.FS

func Catalog() fs.FS  { return mustSub("catalog") }
func Pitfalls() fs.FS { return mustSub("pitfalls") }

func mustSub(dir string) fs.FS {
	sub, err := fs.Sub(files, dir)
	if err != nil {
		panic(err) // the embedded layout is fixed at compile time
	}
	return sub
}
