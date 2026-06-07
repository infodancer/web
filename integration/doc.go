// Package integration hosts cross-component tests for the infodancer/web
// suite. Every component is Postgres-backed and shares one database via
// prefixed tables and per-component goose version tables, so the same database
// can host search, faq, timeline, media, notify, contact, and newsletter side
// by side. The tests cover two layers:
//
//   - Store layer: each component's store methods are driven directly and
//     asserted against the shared database, including that their migrations
//     coexist without colliding (see TestComponentsShareOneDatabase).
//   - HTTP layer: TestSmokeAllComponents boots every implemented component's
//     real handler, mounts them together on one server, and probes every route
//     with the smoke/smolder runner (github.com/infodancer/smoke). This catches
//     wiring, dependency-injection, template, and config faults that 500 on
//     first request even when migrations and stores are green.
//
// The tests require a Postgres reachable at TEST_DATABASE_URL and are skipped
// when it is unset, so a plain `go test` without a database passes; CI sets it
// to the workflow's Postgres service.
package integration
