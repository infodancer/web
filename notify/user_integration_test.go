//go:build integration

package notify

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// player builds a recipient identity in the osg actor scheme.
func player(id string) Identity {
	return Identity{Issuer: "osg", Subject: "user:" + id, Label: "player " + id}
}

func userEvent(title string, recipients ...Identity) Event {
	return Event{
		Type:       "campaign.updated",
		Source:     "osg",
		Title:      title,
		Audience:   AudienceUser,
		Recipients: recipients,
	}
}

func TestEmitFansOutToRecipients(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()
	owner, plyr := player("1"), player("2")

	id, err := s.Insert(ctx, userEvent("Session added", owner, plyr))
	require.NoError(t, err)
	require.Positive(t, id)

	for _, r := range []Identity{owner, plyr} {
		feed, err := s.UserFeed(ctx, r, UserFilter{})
		require.NoError(t, err)
		require.Len(t, feed, 1)
		require.Equal(t, id, feed[0].ID)
		require.Equal(t, "Session added", feed[0].Title)
		require.Nil(t, feed[0].ReadAt)
		require.Nil(t, feed[0].DismissedAt)

		n, err := s.UserUnreadCount(ctx, r)
		require.NoError(t, err)
		require.Equal(t, 1, n)
	}

	// A user-audience event must not surface in the admin firehose.
	adminFeed, err := s.AdminFeed(ctx, Filter{})
	require.NoError(t, err)
	require.Empty(t, adminFeed)
}

func TestEmitWithoutRecipientsWritesNoUserRows(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()

	_, err := s.Insert(ctx, Event{Type: "campaign.created", Source: "osg", Title: "no fan-out"})
	require.NoError(t, err)

	feed, err := s.UserFeed(ctx, player("1"), UserFilter{})
	require.NoError(t, err)
	require.Empty(t, feed)

	n, err := s.UserUnreadCount(ctx, player("1"))
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestUserFeedIsolatesRecipients(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()
	a, b := player("1"), player("2")

	_, err := s.Insert(ctx, userEvent("only for a", a))
	require.NoError(t, err)
	_, err = s.Insert(ctx, userEvent("for both", a, b))
	require.NoError(t, err)

	feedA, err := s.UserFeed(ctx, a, UserFilter{})
	require.NoError(t, err)
	require.Len(t, feedA, 2)

	feedB, err := s.UserFeed(ctx, b, UserFilter{})
	require.NoError(t, err)
	require.Len(t, feedB, 1)
	require.Equal(t, "for both", feedB[0].Title)
}

func TestMarkReadKeepsFeedDropsUnread(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()
	r := player("1")

	id, err := s.Insert(ctx, userEvent("read me", r))
	require.NoError(t, err)

	require.NoError(t, s.MarkRead(ctx, r, id))

	// Still in the feed, but now stamped read and no longer unread.
	feed, err := s.UserFeed(ctx, r, UserFilter{})
	require.NoError(t, err)
	require.Len(t, feed, 1)
	require.NotNil(t, feed[0].ReadAt)

	n, err := s.UserUnreadCount(ctx, r)
	require.NoError(t, err)
	require.Zero(t, n)

	// MarkRead is idempotent and a no-op for an unrelated recipient/event.
	require.NoError(t, s.MarkRead(ctx, r, id))
	require.NoError(t, s.MarkRead(ctx, player("999"), id))
}

func TestDismissRemovesFromFeed(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()
	r := player("1")

	id, err := s.Insert(ctx, userEvent("dismiss me", r))
	require.NoError(t, err)

	require.NoError(t, s.Dismiss(ctx, r, id))

	feed, err := s.UserFeed(ctx, r, UserFilter{})
	require.NoError(t, err)
	require.Empty(t, feed)

	n, err := s.UserUnreadCount(ctx, r)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestFanOutDedupesAndSkipsEmptySubject(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()
	r := player("1")

	id, err := s.Insert(ctx, userEvent("dedup",
		r, r, // same recipient twice
		Identity{Issuer: "osg", Subject: ""}, // unaddressable, skipped
	))
	require.NoError(t, err)

	feed, err := s.UserFeed(ctx, r, UserFilter{})
	require.NoError(t, err)
	require.Len(t, feed, 1)
	require.Equal(t, id, feed[0].ID)

	// The unique constraint also means exactly one delivery row exists.
	var rows int
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT count(*) FROM notify_user_notifications WHERE event_id = $1`, id).Scan(&rows))
	require.Equal(t, 1, rows)
}

func TestUserFeedKeysetPagination(t *testing.T) {
	s := freshStore(t)
	ctx := context.Background()
	r := player("1")

	insertSpaced(t, s,
		userEvent("a", r), userEvent("b", r), userEvent("c", r),
		userEvent("d", r), userEvent("e", r),
	)

	page1, err := s.UserFeed(ctx, r, UserFilter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.Equal(t, "e", page1[0].Title) // newest first
	require.Equal(t, "d", page1[1].Title)

	page2, err := s.UserFeed(ctx, r, UserFilter{Limit: 2, BeforeID: page1[1].ID})
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.Equal(t, "c", page2[0].Title)
	require.Equal(t, "b", page2[1].Title)
	require.Less(t, page2[0].ID, page1[1].ID, "keyset page must be strictly older")
}
