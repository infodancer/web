// Package integration hosts cross-component tests for the infodancer/web
// suite: it mounts the components together against a single database and
// exercises the interactions between them (for example, content created in
// one component becoming searchable in search).
//
// Today the Postgres-backed components (search, notify) are the ones that can
// share one database with prefixed tables and per-component goose version
// tables. contact and newsletter are still SQLite-backed; folding them into
// the shared-Postgres story is a separate design decision, tracked in the
// suite's CONVENTIONS.md.
package integration
