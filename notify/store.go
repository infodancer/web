package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/notify/db"
)

const (
	defaultFeedLimit = 25
	maxFeedLimit     = 100
)

// Store is the data-access entry point. It wraps a pgx pool and maps between
// the domain [Event] and the generated query layer. A zero Store is unusable;
// construct one with [NewStore].
type Store struct {
	q    *db.Queries
	pool *pgxpool.Pool
}

// NewStore constructs a Store from a pgx connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{q: db.New(pool), pool: pool}
}

// Pool returns the underlying pool. Prefer the typed methods; provided for
// tests and callers that need transactional control.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Insert validates and appends one event, returning its assigned id. When the
// event carries Recipients, the event row and one notify_user_notifications row
// per recipient are written atomically in a single transaction, so a partial
// fan-out can never be observed. With no recipients it is a single statement.
func (s *Store) Insert(ctx context.Context, ev Event) (int64, error) {
	if err := ev.normalize(); err != nil {
		return 0, err
	}
	payload, err := ev.payloadJSON()
	if err != nil {
		return 0, err
	}

	params := db.InsertEventParams{
		Source:   ev.Source,
		Type:     ev.Type,
		Title:    ev.Title,
		Severity: string(ev.Severity),
		Audience: string(ev.Audience),
		Payload:  payload,
		Link:     ev.Link,
	}
	if ev.Actor != nil {
		params.ActorIssuer = nullStr(ev.Actor.Issuer)
		params.ActorSubject = nullStr(ev.Actor.Subject)
		params.ActorLabel = nullStr(ev.Actor.Label)
	}
	if ev.Subject != nil {
		params.SubjectIssuer = nullStr(ev.Subject.Issuer)
		params.SubjectSubject = nullStr(ev.Subject.Subject)
	}

	recipients := validRecipients(ev.Recipients)
	if len(recipients) == 0 {
		row, err := s.q.InsertEvent(ctx, params)
		if err != nil {
			return 0, fmt.Errorf("notify: insert event: %w", err)
		}
		return row.ID, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("notify: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := s.q.WithTx(tx)
	row, err := qtx.InsertEvent(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("notify: insert event: %w", err)
	}
	for _, r := range recipients {
		if err := qtx.InsertUserNotification(ctx, db.InsertUserNotificationParams{
			EventID:          row.ID,
			RecipientIssuer:  r.Issuer,
			RecipientSubject: r.Subject,
		}); err != nil {
			return 0, fmt.Errorf("notify: insert user notification: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("notify: commit fan-out: %w", err)
	}
	return row.ID, nil
}

// validRecipients drops recipients with an empty Subject (an unaddressable row)
// and deduplicates on (issuer, subject) so a caller that lists the same user
// twice produces one delivery row without relying on the ON CONFLICT path.
func validRecipients(in []Identity) []Identity {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[[2]string]struct{}, len(in))
	out := make([]Identity, 0, len(in))
	for _, r := range in {
		if r.Subject == "" {
			continue
		}
		key := [2]string{r.Issuer, r.Subject}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, r)
	}
	return out
}

// Filter constrains an admin feed query. Zero-value fields impose no
// constraint. Limit is clamped to [1, 100]; BeforeID drives keyset pagination
// (rows strictly older than that id).
type Filter struct {
	Source   string
	Type     string
	Severity Severity
	Since    time.Time
	Until    time.Time
	Limit    int
	BeforeID int64
	// AllAudiences widens the feed from the curated admin-audience stream
	// (false, the default) to every event regardless of audience (true) — the
	// full activity view for admins tracking all site activity.
	AllAudiences bool
}

// AdminFeed returns events newest-first for the given filter: the curated
// admin-audience stream by default, or every event when f.AllAudiences is set.
func (s *Store) AdminFeed(ctx context.Context, f Filter) ([]Event, error) {
	lim := f.Limit
	switch {
	case lim <= 0:
		lim = defaultFeedLimit
	case lim > maxFeedLimit:
		lim = maxFeedLimit
	}

	params := db.AdminFeedParams{Lim: int32(lim), AllAudiences: f.AllAudiences}
	if f.Source != "" {
		params.Source = &f.Source
	}
	if f.Type != "" {
		params.Type = &f.Type
	}
	if f.Severity != "" {
		sev := string(f.Severity)
		params.Severity = &sev
	}
	if !f.Since.IsZero() {
		params.Since = &f.Since
	}
	if !f.Until.IsZero() {
		params.Until = &f.Until
	}
	if f.BeforeID > 0 {
		params.BeforeID = &f.BeforeID
	}

	rows, err := s.q.AdminFeed(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("notify: admin feed: %w", err)
	}
	out := make([]Event, 0, len(rows))
	for _, r := range rows {
		ev, err := toEvent(r)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

// AdminUnreadCount returns how many admin events are newer than the admin's
// read cursor. An admin who has never marked anything seen counts everything.
func (s *Store) AdminUnreadCount(ctx context.Context, id Identity) (int, error) {
	cursor, err := s.adminCursor(ctx, id)
	if err != nil {
		return 0, err
	}
	n, err := s.q.AdminUnreadCount(ctx, cursor)
	if err != nil {
		return 0, fmt.Errorf("notify: unread count: %w", err)
	}
	return int(n), nil
}

// AdminMarkSeen advances the admin's read cursor to upTo. The advance is
// monotonic: a stale request with an older instant cannot move the cursor
// backwards. A zero upTo defaults to now so "mark all read" always advances.
func (s *Store) AdminMarkSeen(ctx context.Context, id Identity, upTo time.Time) error {
	if upTo.IsZero() {
		upTo = time.Now()
	}
	err := s.q.AdminMarkSeen(ctx, db.AdminMarkSeenParams{
		Issuer:  id.Issuer,
		Subject: id.Subject,
		UpTo:    upTo,
	})
	if err != nil {
		return fmt.Errorf("notify: mark seen: %w", err)
	}
	return nil
}

// UserNotification is one recipient's view of an event: the event itself plus
// this recipient's read/dismiss state.
type UserNotification struct {
	Event
	ReadAt      *time.Time
	DismissedAt *time.Time
}

// UserFilter constrains a user feed query. Limit is clamped to [1, 100]
// (default 25); BeforeID drives keyset pagination over the event id (rows
// strictly older than that id).
type UserFilter struct {
	Limit    int
	BeforeID int64
}

// UserFeed returns a recipient's notifications newest-first, excluding the ones
// they have dismissed.
func (s *Store) UserFeed(ctx context.Context, recipient Identity, f UserFilter) ([]UserNotification, error) {
	lim := f.Limit
	switch {
	case lim <= 0:
		lim = defaultFeedLimit
	case lim > maxFeedLimit:
		lim = maxFeedLimit
	}

	params := db.UserFeedParams{
		RecipientIssuer:  recipient.Issuer,
		RecipientSubject: recipient.Subject,
		Lim:              int32(lim),
	}
	if f.BeforeID > 0 {
		params.BeforeID = &f.BeforeID
	}

	rows, err := s.q.UserFeed(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("notify: user feed: %w", err)
	}
	out := make([]UserNotification, 0, len(rows))
	for _, r := range rows {
		payload, err := decodePayload(r.Payload)
		if err != nil {
			return nil, err
		}
		out = append(out, UserNotification{
			Event: Event{
				ID:        r.ID,
				Type:      r.Type,
				Source:    r.Source,
				Title:     r.Title,
				Severity:  Severity(r.Severity),
				Audience:  Audience(r.Audience),
				Link:      r.Link,
				Actor:     ident(r.ActorIssuer, r.ActorSubject, r.ActorLabel),
				Subject:   ident(r.SubjectIssuer, r.SubjectSubject, nil),
				Payload:   payload,
				CreatedAt: r.CreatedAt,
			},
			ReadAt:      r.ReadAt,
			DismissedAt: r.DismissedAt,
		})
	}
	return out, nil
}

// UserUnreadCount returns how many of a recipient's notifications are neither
// read nor dismissed.
func (s *Store) UserUnreadCount(ctx context.Context, recipient Identity) (int, error) {
	n, err := s.q.UserUnreadCount(ctx, db.UserUnreadCountParams{
		RecipientIssuer:  recipient.Issuer,
		RecipientSubject: recipient.Subject,
	})
	if err != nil {
		return 0, fmt.Errorf("notify: user unread count: %w", err)
	}
	return int(n), nil
}

// MarkRead stamps the recipient's view of eventID as read. It is idempotent —
// the first read instant is kept — and a no-op if the recipient has no row for
// that event.
func (s *Store) MarkRead(ctx context.Context, recipient Identity, eventID int64) error {
	err := s.q.MarkUserRead(ctx, db.MarkUserReadParams{
		RecipientIssuer:  recipient.Issuer,
		RecipientSubject: recipient.Subject,
		EventID:          eventID,
	})
	if err != nil {
		return fmt.Errorf("notify: mark read: %w", err)
	}
	return nil
}

// Dismiss stamps the recipient's view of eventID as dismissed, removing it from
// UserFeed and the unread count. Idempotent; a no-op when there is no such row.
func (s *Store) Dismiss(ctx context.Context, recipient Identity, eventID int64) error {
	err := s.q.DismissUser(ctx, db.DismissUserParams{
		RecipientIssuer:  recipient.Issuer,
		RecipientSubject: recipient.Subject,
		EventID:          eventID,
	})
	if err != nil {
		return fmt.Errorf("notify: dismiss: %w", err)
	}
	return nil
}

// adminCursor returns the admin's last_seen_at, or the zero time (before any
// event) when the admin has no cursor row yet.
func (s *Store) adminCursor(ctx context.Context, id Identity) (time.Time, error) {
	t, err := s.q.AdminCursor(ctx, db.AdminCursorParams{Issuer: id.Issuer, Subject: id.Subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("notify: read cursor: %w", err)
	}
	return t, nil
}

// nullStr maps an empty string to a SQL NULL and any other string to itself,
// so absent identity fields are stored as NULL rather than "".
func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// toEvent maps a stored row back to the domain Event.
func toEvent(r db.NotifyEvents) (Event, error) {
	payload, err := decodePayload(r.Payload)
	if err != nil {
		return Event{}, err
	}
	return Event{
		ID:        r.ID,
		Type:      r.Type,
		Source:    r.Source,
		Title:     r.Title,
		Severity:  Severity(r.Severity),
		Audience:  Audience(r.Audience),
		Link:      r.Link,
		Actor:     ident(r.ActorIssuer, r.ActorSubject, r.ActorLabel),
		Subject:   ident(r.SubjectIssuer, r.SubjectSubject, nil),
		Payload:   payload,
		CreatedAt: r.CreatedAt,
	}, nil
}

// ident reconstructs an Identity from nullable columns, returning nil when all
// are absent.
func ident(issuer, subject, label *string) *Identity {
	if issuer == nil && subject == nil && label == nil {
		return nil
	}
	id := &Identity{}
	if issuer != nil {
		id.Issuer = *issuer
	}
	if subject != nil {
		id.Subject = *subject
	}
	if label != nil {
		id.Label = *label
	}
	return id
}
