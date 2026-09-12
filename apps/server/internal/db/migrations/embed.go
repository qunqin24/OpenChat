// Package migrations embeds the goose SQL migrations into the binary so the
// server can migrate itself on start (or via `openchat migrate`).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
