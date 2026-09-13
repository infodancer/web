package notify

import (
	"context"
	"testing"
	"time"

	"github.com/infodancer/web/pgtest"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// testPool is the current test's database, set by freshStore. It is package
// level so the tests that query it directly need no change, which is sound only
// because no database test in this package calls t.Parallel.
var testPool *pgxpool.Pool

// freshStore returns a clean Store on this test's database. The database comes
// from pgtest.Shared: created and migrated on the first call, reused by later
// calls in the same test, and skipped when WEB_TEST_PG is unset.
func freshStore(t *testing.T) *Store {
	t.Helper()
	testPool = pgtest.Shared(t, pgtest.WithMigrate(Migrate))
	_, err := testPool.Exec(context.Background(),
		`TRUNCATE notify_events, notify_user_notifications, notify_admin_read_cursor RESTART IDENTITY`)
	require.NoError(t, err)
	return NewStore(testPool)
}

// admin is a fixed test identity.
func admin() Identity {
	return Identity{Issuer: "https://test.example", Subject: "admin-1", Label: "admin@test"}
}

// insertSpaced inserts events one at a time with a small gap so created_at is
// strictly increasing, which the cursor assertions rely on.
func insertSpaced(t *testing.T, s *Store, evs ...Event) []int64 {
	t.Helper()
	ids := make([]int64, 0, len(evs))
	for _, ev := range evs {
		id, err := s.Insert(context.Background(), ev)
		require.NoError(t, err)
		ids = append(ids, id)
		time.Sleep(2 * time.Millisecond)
	}
	return ids
}

func TestInsertAndFeedRoundTrip(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	in := Event{
		Type:     "user.created",
		Source:   "osg",
		Title:    "New account: Ada",
		Severity: SeverityNotice,
		Link:     "https://osg.example/users/ada",
		Actor:    &Identity{Issuer: "https://idp", Subject: "sub-9", Label: "ada@osg"},
		Payload:  map[string]any{"handle": "ada", "verified": true},
	}
	id, err := s.Insert(ctx, in)
	require.NoError(t, err)
	require.Positive(t, id)

	feed, err := s.AdminFeed(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, feed, 1)

	got := feed[0]
	require.Equal(t, id, got.ID)
	require.Equal(t, "user.created", got.Type)
	require.Equal(t, "osg", got.Source)
	require.Equal(t, SeverityNotice, got.Severity)
	require.Equal(t, AudienceAdmin, got.Audience) // defaulted
	require.Equal(t, "https://osg.example/users/ada", got.Link)
	require.NotNil(t, got.Actor)
	require.Equal(t, "ada@osg", got.Actor.Label)
	require.Equal(t, "ada", got.Payload["handle"])
	require.Equal(t, true, got.Payload["verified"])
	require.False(t, got.CreatedAt.IsZero())
}

func TestAdminFeedFilters(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	insertSpaced(t, s,
		Event{Type: "user.created", Source: "osg", Title: "a", Severity: SeverityInfo},
		Event{Type: "campaign.created", Source: "osg", Title: "b", Severity: SeverityWarn},
		Event{Type: "user.created", Source: "other", Title: "c", Severity: SeverityAlert},
	)

	byType, err := s.AdminFeed(ctx, Filter{Type: "user.created"})
	require.NoError(t, err)
	require.Len(t, byType, 2)

	bySource, err := s.AdminFeed(ctx, Filter{Source: "other"})
	require.NoError(t, err)
	require.Len(t, bySource, 1)
	require.Equal(t, "c", bySource[0].Title)

	bySeverity, err := s.AdminFeed(ctx, Filter{Severity: SeverityWarn})
	require.NoError(t, err)
	require.Len(t, bySeverity, 1)
	require.Equal(t, "b", bySeverity[0].Title)
}

func TestAdminFeedExcludesUserAudience(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	insertSpaced(t, s,
		Event{Type: "x", Source: "osg", Title: "admin one", Audience: AudienceAdmin},
		Event{Type: "x", Source: "osg", Title: "user one", Audience: AudienceUser},
	)

	feed, err := s.AdminFeed(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, feed, 1)
	require.Equal(t, "admin one", feed[0].Title)
}

func TestAdminFeedAllAudiencesIncludesEverything(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	insertSpaced(t, s,
		Event{Type: "x", Source: "osg", Title: "admin one", Audience: AudienceAdmin},
		Event{Type: "x", Source: "osg", Title: "user one", Audience: AudienceUser},
	)

	// AllAudiences widens the curated admin stream to the full firehose.
	feed, err := s.AdminFeed(ctx, Filter{AllAudiences: true})
	require.NoError(t, err)
	require.Len(t, feed, 2)

	// Per-field filters still apply alongside the widened audience.
	byType, err := s.AdminFeed(ctx, Filter{AllAudiences: true, Source: "osg", Type: "x"})
	require.NoError(t, err)
	require.Len(t, byType, 2)
}

func TestAdminFeedKeysetPagination(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	evs := make([]Event, 0, 5)
	for i := range 5 {
		evs = append(evs, Event{Type: "t", Source: "osg", Title: string(rune('a' + i))})
	}
	insertSpaced(t, s, evs...)

	page1, err := s.AdminFeed(ctx, Filter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.Equal(t, "e", page1[0].Title) // newest first
	require.Equal(t, "d", page1[1].Title)

	page2, err := s.AdminFeed(ctx, Filter{Limit: 2, BeforeID: page1[1].ID})
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.Equal(t, "c", page2[0].Title)
	require.Equal(t, "b", page2[1].Title)

	require.Less(t, page2[0].ID, page1[1].ID, "keyset page must be strictly older")
}

func TestAdminFeedLimitClamp(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	evs := make([]Event, 0, 30)
	for range 30 {
		evs = append(evs, Event{Type: "t", Source: "osg", Title: "x"})
	}
	insertSpaced(t, s, evs...)

	def, err := s.AdminFeed(ctx, Filter{Limit: 0})
	require.NoError(t, err)
	require.Len(t, def, defaultFeedLimit)

	capped, err := s.AdminFeed(ctx, Filter{Limit: 1000})
	require.NoError(t, err)
	require.Len(t, capped, 30) // only 30 exist, but the query asked for <= 100
}

func TestAdminUnreadAndMarkSeen(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()
	a := admin()

	insertSpaced(t, s,
		Event{Type: "t", Source: "osg", Title: "1"},
		Event{Type: "t", Source: "osg", Title: "2"},
		Event{Type: "t", Source: "osg", Title: "3"},
	)

	// A never-seen admin counts everything.
	n, err := s.AdminUnreadCount(ctx, a)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	feed, err := s.AdminFeed(ctx, Filter{}) // [3,2,1] newest first
	require.NoError(t, err)
	require.Len(t, feed, 3)
	second := feed[1] // title "2"

	// Mark seen up to the second-newest → only the newest remains unread.
	require.NoError(t, s.AdminMarkSeen(ctx, a, second.CreatedAt))
	n, err = s.AdminUnreadCount(ctx, a)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// A stale mark with an older instant must not move the cursor backwards.
	oldest := feed[2]
	require.NoError(t, s.AdminMarkSeen(ctx, a, oldest.CreatedAt))
	n, err = s.AdminUnreadCount(ctx, a)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// Marking up to the newest clears the count.
	newest := feed[0]
	require.NoError(t, s.AdminMarkSeen(ctx, a, newest.CreatedAt))
	n, err = s.AdminUnreadCount(ctx, a)
	require.NoError(t, err)
	require.Equal(t, 0, n)
}

func TestMigrateIdempotent(t *testing.T) {
	pool := pgtest.Shared(t, pgtest.WithMigrate(Migrate))
	// Running Migrate again against the already-migrated pool must be a no-op.
	require.NoError(t, Migrate(context.Background(), pool))
}
