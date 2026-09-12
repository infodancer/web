package pgtest_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/infodancer/web/pgtest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// requireServer skips when no test server is configured.
//
// Pool and DSN skip on their own, which is enough for a test that calls them
// once on the test goroutine. It is not enough in three places: a skip raised
// inside a spawned goroutine unwinds only that goroutine and leaves the test
// asserting on values it never received; a test that drives the failure path of
// a fake T never reaches it, because the skip happens first; and a test that
// asserts a database is gone passes for the wrong reason when there was never a
// database at all.
func requireServer(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(pgtest.EnvVar)) == "" {
		t.Skipf("%s is not set", pgtest.EnvVar)
	}
}

// TestPoolGivesAWorkingDatabase is the basic contract: a caller asks for a
// database and gets one it can write to.
func TestPoolGivesAWorkingDatabase(t *testing.T) {
	pool := pgtest.Pool(t)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE widgets (id int primary key, name text)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO widgets VALUES (1, 'sprocket')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var name string
	if err := pool.QueryRow(ctx, `SELECT name FROM widgets WHERE id = 1`).Scan(&name); err != nil {
		t.Fatalf("select: %v", err)
	}
	if name != "sprocket" {
		t.Errorf("name = %q, want sprocket", name)
	}
}

// TestEachTestGetsItsOwnDatabase is the isolation property, and the reason this
// exists rather than a shared scratch database: two tests running against one
// server must not see each other's tables, so they can run in any order and in
// parallel.
func TestEachTestGetsItsOwnDatabase(t *testing.T) {
	ctx := context.Background()

	first := pgtest.Pool(t)
	if _, err := first.Exec(ctx, `CREATE TABLE only_in_first (id int)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	second := pgtest.Pool(t)
	var exists bool
	err := second.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'only_in_first')`,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if exists {
		t.Error("the second database can see the first database's table; they are not isolated")
	}

	if firstDB, secondDB := pgtest.DSN(t), pgtest.DSN(t); firstDB == secondDB {
		t.Errorf("two calls returned the same DSN (%s); each must name its own database", firstDB)
	}
}

// TestParallelTestsDoNotCollide runs the allocation concurrently, which is how
// a converted suite will use it.
func TestParallelTestsDoNotCollide(t *testing.T) {
	requireServer(t)

	const n = 8
	var wg sync.WaitGroup
	names := make([]string, n)

	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			names[i] = pgtest.DSN(t)
		}()
	}
	wg.Wait()

	seen := make(map[string]bool, n)
	for _, name := range names {
		if name == "" {
			t.Fatal("a concurrent allocation returned no DSN")
		}
		if seen[name] {
			t.Errorf("duplicate database handed out concurrently: %s", name)
		}
		seen[name] = true
	}
}

// TestMigrateRunsBeforeTheCallerSeesThePool covers how components actually set
// up: each owns embedded goose migrations exposed as Migrate(ctx, dsn).
func TestMigrateRunsBeforeTheCallerSeesThePool(t *testing.T) {
	var ran bool

	// The hook takes the pool because that is how components migrate: a module
	// is built with Config{DB: pool} and exposes Migrate(ctx).
	pool := pgtest.Pool(t, pgtest.WithMigrate(func(ctx context.Context, p *pgxpool.Pool) error {
		ran = true
		_, err := p.Exec(ctx, `CREATE TABLE migrated (id int primary key)`)
		return err
	}))

	if !ran {
		t.Fatal("the migrate hook never ran")
	}

	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'migrated')`,
	).Scan(&exists); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !exists {
		t.Error("the pool does not see the migrated schema; the hook ran against the wrong database")
	}
}

// TestMigrateFailureFailsTheTest pins that a broken migration is reported
// rather than leaving the caller with an empty database and a confusing error
// several lines later.
func TestMigrateFailureFailsTheTest(t *testing.T) {
	requireServer(t)

	fake := &fakeT{T: t}
	func() {
		defer func() { _ = recover() }() // Fatalf on a fake T unwinds via panic
		pgtest.Pool(fake, pgtest.WithMigrate(func(context.Context, *pgxpool.Pool) error {
			return errors.New("migration exploded")
		}))
	}()

	if !fake.failed {
		t.Error("a failing migration did not fail the test")
	}
	if !strings.Contains(fake.msg, "migration exploded") {
		t.Errorf("failure message does not carry the migration error: %q", fake.msg)
	}
}

// TestSkipsWhenNoServerConfigured is the behaviour on a machine with no test
// Postgres: skip with a message naming the variable, never fail and never
// silently pass having tested nothing.
func TestSkipsWhenNoServerConfigured(t *testing.T) {
	t.Setenv(pgtest.EnvVar, "")

	fake := &fakeT{T: t}
	func() {
		defer func() { _ = recover() }()
		pgtest.Pool(fake)
	}()

	if !fake.skipped {
		t.Error("with no server configured the helper must skip")
	}
	if fake.failed {
		t.Error("a missing test server is a skip, not a failure")
	}
	if !strings.Contains(fake.msg, pgtest.EnvVar) {
		t.Errorf("skip message does not name %s: %q", pgtest.EnvVar, fake.msg)
	}
}

// TestDatabaseIsDroppedAfterTheTest keeps a long-lived server from filling with
// abandoned databases, which is the cost of not using a throwaway container.
func TestDatabaseIsDroppedAfterTheTest(t *testing.T) {
	requireServer(t)

	var dsn string
	t.Run("inner", func(t *testing.T) {
		dsn = pgtest.DSN(t)
	})

	// The subtest has returned, so its cleanup has run.
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err == nil {
		_ = conn.Close(ctx)
		t.Fatalf("the test database still exists after the test finished: %s", dsn)
	}
}

// fakeT records what the helper did to a *testing.T without failing the real
// one. Fatalf and SkipNow must not return, so both panic; the caller recovers.
type fakeT struct {
	*testing.T
	failed  bool
	skipped bool
	msg     string
}

func (f *fakeT) Fatalf(format string, args ...any) {
	f.failed = true
	f.msg = fmt.Sprintf(format, args...)
	panic("fatal")
}

func (f *fakeT) Fatal(args ...any) {
	f.failed = true
	f.msg = fmt.Sprintln(args...)
	panic("fatal")
}

func (f *fakeT) Skipf(format string, args ...any) {
	f.skipped = true
	f.msg = fmt.Sprintf(format, args...)
	panic("skip")
}

func (f *fakeT) Skip(args ...any) {
	f.skipped = true
	f.msg = fmt.Sprintln(args...)
	panic("skip")
}

func (f *fakeT) Cleanup(func()) {} // the fake never runs cleanups

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
