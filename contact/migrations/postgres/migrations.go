// Package postgres embeds PostgreSQL migration files for the contact module.
package postgres

import "embed"

//go:embed *.sql
var FS embed.FS
