// Package migrations embeds the versioned SQL migrations (goose format).
// Never edit a migration that has shipped; add a new one instead (PRD §111).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
