// Package postgres embeds PostgreSQL migration files for the newsletter module.
package postgres

import "embed"

//go:embed *.sql
var FS embed.FS
