// Command search is the CLI for the search module: a long-running HTTP server
// (serve), a dev corpus seeder (seed), and the indexer (index, not yet built).
// See DESIGN.md §9.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(ctx, os.Args[2:])
	case "seed":
		err = runSeed(ctx, os.Args[2:])
	case "index":
		err = runIndex(ctx, os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		//nolint:gosec // G705: os.Args echoed to stderr in a CLI diagnostic — not an HTTP response, no XSS surface.
		fmt.Fprintf(os.Stderr, "search: unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "search: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `search — Postgres-backed search for InfoDancer/matthewjhunter sites

Usage:
  search serve  [--config FILE] [--db DSN] [--addr :8080] [--base-path /search] [--migrate]
  search seed   [--config FILE] [--db DSN] [--file testdata/seed.sql]
  search index  [flags]   (not yet implemented; see DESIGN.md)

The DSN may also come from --config (db.query_dsn for serve, db.indexer_dsn for
seed) or the DATABASE_URL environment variable. Flags take precedence.
`)
}
