//go:build integration

package errnotice

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/infodancer/web/obs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	pg, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("errnotice_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Fatalf("postgres container: %v", err)
	}

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("connection string: %v", err)
	}

	testPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("pool: %v", err)
	}

	if err := Migrate(ctx, testPool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	code := m.Run()

	testPool.Close()
	_ = pg.Terminate(ctx)
	os.Exit(code)
}

// fresh truncates the table and returns a clean Reporter over the pool.
func fresh(t *testing.T) *Reporter {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `TRUNCATE error_notices`)
	require.NoError(t, err)
	return New(testPool)
}

func TestReporter_DedupBumpsCount(t *testing.T) {
	r := fresh(t)
	ctx := context.Background()

	first := obs.Notice{
		RequestID: "req-1",
		Component: "blog",
		Method:    "GET",
		Path:      "/posts/123",
		Status:    500,
	}
	// Same logical failure (an id segment differs), reported again with a new
	// request id -- must collapse onto the one row.
	second := first
	second.RequestID = "req-2"
	second.Path = "/posts/456"

	r.Report(ctx, first)
	r.Report(ctx, second)

	got, err := r.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 1, "the same logical failure must be one row")

	row := got[0]
	require.Equal(t, int64(2), row.Count, "a repeat bumps count")
	require.Equal(t, "req-2", row.RequestID, "last-occurrence request id wins")
	require.Equal(t, "/posts/456", row.Path, "last-occurrence path wins")
	require.Equal(t, 500, row.Status)
	require.False(t, row.LastSeen.Before(row.FirstSeen), "last_seen never precedes first_seen")
}

func TestReporter_DistinctFailureNewRow(t *testing.T) {
	r := fresh(t)
	ctx := context.Background()

	a := obs.Notice{Component: "blog", Method: "GET", Path: "/posts/1", Status: 500}
	b := obs.Notice{Component: "profile", Method: "POST", Path: "/profile/save", Status: 503}

	r.Report(ctx, a)
	r.Report(ctx, b)

	got, err := r.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 2, "a genuinely different failure is a second row")
}

func TestReporter_ListNewestFirst(t *testing.T) {
	r := fresh(t)
	ctx := context.Background()

	// older, then newer -- List orders by last_seen DESC.
	older := obs.Notice{Component: "blog", Method: "GET", Path: "/a", Status: 500}
	newer := obs.Notice{Component: "blog", Method: "GET", Path: "/b", Status: 500}

	r.Report(ctx, older)
	r.Report(ctx, newer)

	got, err := r.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "/b", got[0].Path, "most recently seen comes first")
	require.Equal(t, "/a", got[1].Path)
	require.False(t, got[0].LastSeen.Before(got[1].LastSeen))
}

func TestReporter_RecordsPanic(t *testing.T) {
	r := fresh(t)
	ctx := context.Background()

	r.Report(ctx, obs.Notice{
		Component: "blog",
		Method:    "GET",
		Path:      "/boom",
		Status:    500,
		Panic:     "kaboom",
		Stack:     []byte("goroutine 1 [running]:\nmain.h(...)\n\t/app/h.go:10 +0x1\n"),
	})

	got, err := r.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "kaboom", got[0].PanicValue)
	require.NotEmpty(t, got[0].Stack)
}

func TestReporter_ReportSurvivesCancelledContext(t *testing.T) {
	r := fresh(t)
	// The client disconnected: the request context is already cancelled. The
	// write must still land because Report detaches the context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r.Report(ctx, obs.Notice{Component: "blog", Method: "GET", Path: "/x", Status: 500})

	got, err := r.List(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, got, 1, "a cancelled request context must not drop the record")
}

func TestMigrate_Idempotent(t *testing.T) {
	// TestMain already migrated; a second call must be a no-op.
	require.NoError(t, Migrate(context.Background(), testPool))
}
