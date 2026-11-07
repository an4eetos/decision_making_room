// Package migrations embeds the SQL schema so the binary can migrate itself
// on startup. A self-hosted user should never have to install goose.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
