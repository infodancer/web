package integration

import (
	"context"
	"testing"

	smoke "github.com/infodancer/smoke"
)

// TestSmokeAllComponents boots every implemented web component against the
// shared Postgres, mounted together on one server, and probes every route. It
// catches the class of bug the store-level tests cannot: a handler that 500s on
// first request because of a wiring, dependency-injection, template, or config
// fault even though migrations applied and the store works.
//
// Two passes mirror the proven osg/herald usage: reads concurrently, then
// writes (contact, newsletter) serially. A 5xx, a transport error, or a 4xx on
// a route we deliberately probe is a failure; nothing should be silently
// skipped except routes we did not register.
func TestSmokeAllComponents(t *testing.T) {
	pool := newTestPool(t) // skips when WEB_TEST_PG is unset
	app := buildSmokeApp(t, pool)
	ctx := context.Background()

	reads, err := smoke.Run(ctx, app.manifest, smoke.RunOptions{
		BaseURL:       app.server.URL,
		Target:        smoke.Preview,
		IncludeWrites: false,
	})
	if err != nil {
		t.Fatalf("smoke.Run reads: %v", err)
	}

	writes, err := smoke.Run(ctx, app.manifest, smoke.RunOptions{
		BaseURL:       app.server.URL,
		Target:        smoke.Preview,
		IncludeWrites: true,
		Concurrency:   1, // serial: writes mutate the shared database
	})
	if err != nil {
		t.Fatalf("smoke.Run writes: %v", err)
	}

	rp, rf, rs := reads.Counts()
	wp, wf, ws := writes.Counts()
	t.Logf("smoke: reads %d pass / %d fail / %d skip; writes %d pass / %d fail / %d skip",
		rp, rf, rs, wp, wf, ws)

	for _, res := range reads.Failed() {
		t.Errorf("read FAIL %s %s -> %d: %s", res.Method, res.Pattern, res.Status, res.Reason)
	}
	// The writes pass re-probes every route; only count its mutating failures so
	// a read failure is not reported twice.
	for _, res := range writes.Failed() {
		if res.Method == "GET" || res.Method == "HEAD" {
			continue
		}
		t.Errorf("write FAIL %s %s -> %d: %s", res.Method, res.Pattern, res.Status, res.Reason)
	}

	if rp == 0 {
		t.Error("no read routes passed; harness is not exercising any handler")
	}
	if wp == 0 {
		t.Error("no write routes passed; contact/newsletter probes did not run")
	}
}
