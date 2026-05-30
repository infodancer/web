// Package integration hosts cross-component tests for the infodancer/web
// suite: it mounts the components together against a single database and
// exercises the interactions between them (for example, content created in
// one component becoming searchable in search).
//
// Every component is Postgres-backed and shares one database via prefixed
// tables and per-component goose version tables, so the same database can host
// search, notify, contact, and newsletter side by side for these tests.
package integration
