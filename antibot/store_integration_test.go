package antibot

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/antibot/db"
	"github.com/infodancer/web/pgtest"
)

// migratedPool gives the test its own migrated database on the WEB_TEST_PG
// server, dropped when the test ends. The limiter's guarantees are properties
// of the SQL -- the atomic upsert, the window key, the sweep -- so they are
// tested against real Postgres rather than trusted to the fake. With
// WEB_TEST_PG unset these tests skip; the Check logic they rest on is covered
// by the unit tests, which need no database.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return pgtest.Pool(t, pgtest.WithMigrate(Migrate))
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := migratedPool(t)
	if err := Migrate(context.Background(), pool); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

// The whole limit rests on this: concurrent submissions on one key must each
// see a distinct count. A read-then-write would let several read the same count
// and all slip under the limit.
func TestHitIsAtomicUnderConcurrency(t *testing.T) {
	q := db.New(migratedPool(t))
	p := db.HitParams{
		Form: "contact", KeyKind: "ip", KeyHash: make([]byte, 32),
		WindowStart: t0, ExpiresAt: t0.Add(time.Hour),
	}
	const n = 50

	var mu sync.Mutex
	var got []int
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits, err := q.Hit(context.Background(), p)
			if err != nil {
				t.Errorf("Hit: %v", err)
				return
			}
			mu.Lock()
			got = append(got, int(hits))
			mu.Unlock()
		}()
	}
	wg.Wait()

	sort.Ints(got)
	for i, h := range got {
		if h != i+1 {
			t.Fatalf("counts %v: want each of 1..%d exactly once", got, n)
		}
	}
}

// End to end against Postgres: with many concurrent submissions from one
// address, exactly Max get through.
func TestGuardAllowsExactlyTheLimitUnderConcurrency(t *testing.T) {
	c := &clock{now: t0}
	g, err := New(Config{
		DB: migratedPool(t), Secret: testSecret, Now: c.Now, LookupMX: goodMX().LookupMX,
		IPLimit: Limit{Max: 5, Window: time.Hour},
	})
	if err != nil {
		t.Fatal(err)
	}
	tok := g.Token("contact")
	c.Advance(10 * time.Second)

	var mu sync.Mutex
	allowed := 0
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := request{remote: "203.0.113.7:5555", fields: map[string][]string{defaultTokenField: {tok}}}.build(t)
			v, err := g.Check(context.Background(), r, "contact")
			if err != nil {
				t.Errorf("Check: %v", err)
				return
			}
			if v.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed != 5 {
		t.Fatalf("%d of 25 concurrent submissions allowed, want exactly 5", allowed)
	}
}

func TestSweepDeletesOnlyExpiredWindows(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	q := db.New(pool)
	expired := db.HitParams{Form: "contact", KeyKind: "ip", KeyHash: []byte("expired-key-expired-key-expired!"),
		WindowStart: t0.Add(-2 * time.Hour), ExpiresAt: t0.Add(-time.Hour)}
	live := db.HitParams{Form: "contact", KeyKind: "ip", KeyHash: []byte("live-key-live-key-live-key-live!"),
		WindowStart: t0, ExpiresAt: t0.Add(time.Hour)}
	for _, p := range []db.HitParams{expired, live} {
		if _, err := q.Hit(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	c := &clock{now: t0}
	g, err := New(Config{DB: pool, Secret: testSecret, Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	n, err := g.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 1 {
		t.Errorf("Sweep removed %d rows, want 1", n)
	}

	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM antibot_counters`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("after Sweep: %d rows, want the 1 live window", remaining)
	}
}
