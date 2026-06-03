package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/infodancer/web/contact/submissions"
	"github.com/infodancer/web/timeline"
	"github.com/infodancer/web/timeline/timelineuser"
)

// stubResolver is an always-anonymous resolver — enough to construct the
// timeline module for a migration coexistence check.
type stubResolver struct{}

func (stubResolver) Resolve(context.Context, *http.Request) (*timelineuser.User, error) {
	return nil, nil
}

// TestTimelineCoexistsInSharedDatabase verifies the timeline component migrates
// into the same database as the others without colliding: its prefixed tables
// and its dedicated goose version table sit alongside contact's.
func TestTimelineCoexistsInSharedDatabase(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)

	if err := submissions.Migrate(ctx, pool); err != nil {
		t.Fatalf("contact migrate: %v", err)
	}
	m, err := timeline.New(timeline.Config{DB: pool, Resolver: stubResolver{}})
	if err != nil {
		t.Fatalf("timeline new: %v", err)
	}
	if err := m.Migrate(ctx); err != nil {
		t.Fatalf("timeline migrate: %v", err)
	}

	for _, table := range []string{
		"contact_submissions",
		"timeline_timelines",
		"timeline_events",
		"timeline_users",
		"timeline_goose_db_version",
		"contact_goose_db_version",
	} {
		var exists bool
		if err := pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = $1)",
			table,
		).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %q to exist in the shared database", table)
		}
	}
}
