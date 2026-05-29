// Package sqlite embeds SQLite migration files for the newsletter module.
package sqlite

import "embed"

//go:embed *.sql
var FS embed.FS
