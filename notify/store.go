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

// Insert validates and appends one event, returning its assigned id.
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

	row, err := s.q.InsertEvent(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("notify: insert event: %w", err)
	}
	return row.ID, nil
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
}

// AdminFeed returns admin-audience events newest-first for the given filter.
func (s *Store) AdminFeed(ctx context.Context, f Filter) ([]Event, error) {
	lim := f.Limit
	switch {
	case lim <= 0:
		lim = defaultFeedLimit
	case lim > maxFeedLimit:
		lim = maxFeedLimit
	}

	params := db.AdminFeedParams{Lim: int32(lim)}
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
