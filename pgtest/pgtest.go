// Package pgtest gives a test its own PostgreSQL database on a server that
// already exists.
//
// The suite's components are Postgres-backed, so their tests need a real
// database rather than a mock. The alternative in use until now was
// testcontainers, which starts a container per package: that pulls the Docker
// client (and, through it, the Docker Engine module) into every component's
// dependency graph, requires a working Docker daemon to run `go vet`, and pays
// for a container start on every package.
//
// Here a test names a server through the WEB_TEST_PG environment variable and
// gets a freshly created database of its own, dropped when the test finishes.
// Tests stay isolated from each other and can run in parallel, and the only
// dependency is pgx.
//
// With the variable unset the helper skips rather than fails, so `go test` on a
// machine with no test server reports skipped tests instead of red ones. That
// cuts both ways: an unset variable means the database-backed tests did not run
// at all, so CI must set it.
//
//	func TestThing(t *testing.T) {
//	    pool := pgtest.Pool(t, pgtest.WithMigrate(func(ctx context.Context, p *pgxpool.Pool) error {
//	        m, err := thing.New(thing.Config{DB: p})
//	        if err != nil {
//	            return err
//	        }
//	        return m.Migrate(ctx)
//	    }))
//	    // ... use pool
//	}
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnvVar names the environment variable holding the DSN of the PostgreSQL
// server to create test databases on. The DSN's own database is used only to
// connect while issuing CREATE DATABASE; nothing is written to it.
const EnvVar = "WEB_TEST_PG"

// maxIdentifier is PostgreSQL's identifier length limit. A name longer than
// this is silently truncated by the server, which would turn two long test
// names into one database and break the isolation this package exists for.
const maxIdentifier = 63

// TestingT is the part of *testing.T this package uses. It is an interface
// rather than *testing.T so the package can test its own failure and skip
// paths; testing.TB cannot be implemented outside the testing package.
type TestingT interface {
	Helper()
	Name() string
	Fatalf(format string, args ...any)
	Skipf(format string, args ...any)
	Cleanup(func())
}

type config struct {
	migrate func(context.Context, *pgxpool.Pool) error
}

// An Option configures database creation.
type Option func(*config)

// WithMigrate runs fn against the new database before the caller sees it, and
// fails the test if fn returns an error. The hook takes a pool because that is
// how components migrate: built with Config{DB: pool}, then Migrate(ctx).
func WithMigrate(fn func(ctx context.Context, pool *pgxpool.Pool) error) Option {
	return func(c *config) { c.migrate = fn }
}

// Pool creates a database for this test and returns a pool connected to it.
// The pool is closed and the database dropped when the test finishes.
//
// The test is skipped when EnvVar is unset, and failed when the server is
// configured but unreachable: a server that was asked for and did not answer is
// a broken environment, not an absent one.
func Pool(t TestingT, opts ...Option) *pgxpool.Pool {
	t.Helper()

	var cfg config
	for _, o := range opts {
		o(&cfg)
	}

	ctx := context.Background()
	dsn := create(t, ctx)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgtest: connecting to the new database: %v", err)
		return nil
	}
	t.Cleanup(pool.Close)

	if cfg.migrate != nil {
		if err := cfg.migrate(ctx, pool); err != nil {
			t.Fatalf("pgtest: migrating the test database: %v", err)
			return nil
		}
	}
	return pool
}

// shared maps a test to the pool Shared created for it, so later calls from the
// same test return that pool. Entries are removed when the test ends.
var shared sync.Map // TestingT -> *pgxpool.Pool

// Shared returns this test's database: created and migrated on the first call,
// the same pool on every later call from the same test. Options apply to the
// first call only.
//
// It is for suites written against one database per package that a helper
// empties before each test. Such a helper is sometimes called twice in one test,
// and the test keeps using the store it got from the first call. With Pool the
// second call would hand back a different, empty database, so writes through
// the first store would land where anything built on the second never looks.
// Shared keeps both calls on one database, and the helper's own TRUNCATE gives
// the second call the clean slate the test was written against.
func Shared(t TestingT, opts ...Option) *pgxpool.Pool {
	t.Helper()
	if p, ok := shared.Load(t); ok {
		return p.(*pgxpool.Pool)
	}
	pool := Pool(t, opts...)
	if pool == nil {
		return nil // skipped or failed; the test is already over
	}
	shared.Store(t, pool)
	t.Cleanup(func() { shared.Delete(t) })
	return pool
}

// DSN creates a database for this test and returns its connection string, for
// callers that need a DSN rather than a pool (a component whose generated
// queries open their own connection, or a migration tool taking a URL). The
// database is dropped when the test finishes.
func DSN(t TestingT, opts ...Option) string {
	t.Helper()

	var cfg config
	for _, o := range opts {
		o(&cfg)
	}

	ctx := context.Background()
	dsn := create(t, ctx)

	if cfg.migrate != nil {
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			t.Fatalf("pgtest: connecting to the new database: %v", err)
			return ""
		}
		defer pool.Close()
		if err := cfg.migrate(ctx, pool); err != nil {
			t.Fatalf("pgtest: migrating the test database: %v", err)
			return ""
		}
	}
	return dsn
}

// create makes a new database on the configured server and registers its drop.
// It returns the DSN of the new database.
func create(t TestingT, ctx context.Context) string {
	t.Helper()

	serverDSN := os.Getenv(EnvVar)
	if strings.TrimSpace(serverDSN) == "" {
		t.Skipf("pgtest: %s is not set, so no database-backed tests ran; "+
			"point it at a PostgreSQL server, e.g. %s='postgres://user:pass@localhost:5432/postgres?sslmode=disable'",
			EnvVar, EnvVar)
		return ""
	}

	name := databaseName(t.Name())

	admin, err := pgx.Connect(ctx, serverDSN)
	if err != nil {
		t.Fatalf("pgtest: connecting to the server named by %s: %v", EnvVar, err)
		return ""
	}
	defer func() { _ = admin.Close(ctx) }()

	// The name is generated here, not caller-supplied, so quoting it is about
	// surviving a test name with odd characters rather than untrusted input.
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+quoteIdentifier(name)); err != nil {
		t.Fatalf("pgtest: creating database %s: %v", name, err)
		return ""
	}

	t.Cleanup(func() {
		// A fresh connection: the admin one above is closed by then, and the
		// test's own pool may still have been closing when cleanup started.
		// FORCE terminates leftover backends, which a leaked connection in the
		// test would otherwise hold open, blocking the drop and leaking a
		// database onto a long-lived server.
		conn, err := pgx.Connect(context.WithoutCancel(ctx), serverDSN)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
		_, _ = conn.Exec(context.WithoutCancel(ctx),
			`DROP DATABASE IF EXISTS `+quoteIdentifier(name)+` WITH (FORCE)`)
	})

	return replaceDatabase(serverDSN, name)
}

// databaseName builds a unique, legal database name that still says which test
// made it, so a leftover database on a shared server can be traced back.
func databaseName(testName string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(testName) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	suffix := randomHex(8)
	const prefix = "pgtest_"
	// Truncate the readable part, never the suffix: the suffix is what keeps
	// two tests with long shared prefixes from colliding after truncation.
	room := maxIdentifier - len(prefix) - 1 - len(suffix)
	readable := b.String()
	if len(readable) > room {
		readable = readable[:room]
	}
	return prefix + readable + "_" + suffix
}

func randomHex(n int) string {
	buf := make([]byte, n/2)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand does not fail on any supported platform; if it ever
		// does, a panic here is better than silently reusing a name.
		panic("pgtest: reading random bytes: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// replaceDatabase returns serverDSN pointing at database name.
func replaceDatabase(serverDSN, name string) string {
	cfg, err := pgx.ParseConfig(serverDSN)
	if err != nil {
		// Unparseable DSNs are caught by the connect in create, which reports a
		// better error than this function could.
		return serverDSN
	}
	cfg.Database = name

	params := []string{}
	for k, v := range cfg.RuntimeParams {
		params = append(params, k+"="+v)
	}
	sslmode := "disable"
	if cfg.TLSConfig != nil {
		sslmode = "require"
	}
	params = append(params, "sslmode="+sslmode)

	userinfo := cfg.User
	if cfg.Password != "" {
		userinfo += ":" + cfg.Password
	}
	if userinfo != "" {
		userinfo += "@"
	}
	return fmt.Sprintf("postgres://%s%s:%d/%s?%s",
		userinfo, cfg.Host, cfg.Port, name, strings.Join(params, "&"))
}

// quoteIdentifier wraps an identifier in double quotes, doubling any quote it
// contains.
func quoteIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
