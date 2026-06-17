// Package errnotice is the suite's collect-and-record half of the "beyond logs"
// error-reporting design (see CONVENTIONS.md, "Beyond logs: error notices and
// alerting"). It implements obs.ErrorReporter by UPSERTing each captured HTTP
// failure into a Postgres error_notices table, deduplicated by a fingerprint so
// a crash loop is one row with a rising count rather than thousands of rows.
//
// It is host-side glue, never a component dependency: a component logs
// structured ERROR at its boundary and knows nothing of this package. A host
// opts in by constructing a Reporter and passing it to obs.Capture as the
// Reporter. This package records only -- it does not page (alerting is a
// metric-driven, out-of-process concern) and, for now, does not emit a notify
// event (table only).
package errnotice

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/infodancer/web/errnotice/db"
	"github.com/infodancer/web/obs"
	"github.com/jackc/pgx/v5/pgxpool"
)

// writeTimeout bounds the detached UPSERT. The client already has its 500; this
// is durable bookkeeping that must finish quickly or be abandoned.
const writeTimeout = 5 * time.Second

// Reporter records captured failures into the error_notices table. It satisfies
// obs.ErrorReporter and is safe for concurrent use (the pool is, and Reporter
// holds no mutable state).
type Reporter struct {
	q   *db.Queries
	log *slog.Logger
}

// Option configures a Reporter.
type Option func(*Reporter)

// WithLogger injects the logger the Reporter uses to report its own write
// failures. Per the suite's logging rules the host owns logger setup; if unset,
// the Reporter falls back to slog.Default() once, here in the constructor.
func WithLogger(l *slog.Logger) Option {
	return func(r *Reporter) {
		if l != nil {
			r.log = l
		}
	}
}

// New builds a Reporter over the host's pool. Call Migrate(ctx, pool) once
// before serving so the error_notices table exists, then pass the Reporter to
// obs.Capture as its Reporter.
func New(pool *pgxpool.Pool, opts ...Option) *Reporter {
	r := &Reporter{
		q:   db.New(pool),
		log: slog.Default(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Report records one captured failure. It implements obs.ErrorReporter, so it
// returns nothing and never panics: a recording failure is logged at ERROR and
// swallowed.
//
// The write runs on a context detached from the request's (context.WithoutCancel
// + a short timeout). The client that triggered the 500 has already been served
// and may have disconnected, cancelling the request context -- but the failure
// still needs to be recorded, so the write must not ride on a context that is
// about to be cancelled.
func (r *Reporter) Report(ctx context.Context, n obs.Notice) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()

	now := time.Now().UTC()
	var panicValue string
	if n.Panic != nil {
		panicValue = truncate(panicValueString(n.Panic), maxPanicLen)
	}

	err := r.q.UpsertNotice(wctx, db.UpsertNoticeParams{
		Fingerprint: fingerprint(n),
		RequestID:   n.RequestID,
		Component:   n.Component,
		Method:      n.Method,
		Path:        n.Path,
		Status:      clampStatus(n.Status),
		PanicValue:  panicValue,
		Stack:       truncate(string(n.Stack), maxStackLen),
		FirstSeen:   now,
	})
	if err != nil {
		r.log.Error("errnotice: record failed", "err", err, "request_id", n.RequestID)
	}
}

// Record is one recorded failure row, returned by List for the admin "recent
// errors" view. It is the durable shape of an obs.Notice plus the dedup
// bookkeeping (first/last seen and count).
type Record struct {
	ID          int64
	Fingerprint string
	RequestID   string
	Component   string
	Method      string
	Path        string
	Status      int
	PanicValue  string
	Stack       string
	FirstSeen   time.Time
	LastSeen    time.Time
	Count       int64
}

// List returns up to limit recorded failures, most recently active first
// (ordered by last_seen). It backs a future admin "recent errors" view.
func (r *Reporter) List(ctx context.Context, limit int) ([]Record, error) {
	rows, err := r.q.ListRecent(ctx, clampLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("errnotice: list: %w", err)
	}
	out := make([]Record, 0, len(rows))
	for _, row := range rows {
		out = append(out, Record{
			ID:          row.ID,
			Fingerprint: row.Fingerprint,
			RequestID:   row.RequestID,
			Component:   row.Component,
			Method:      row.Method,
			Path:        row.Path,
			Status:      int(row.Status),
			PanicValue:  row.PanicValue,
			Stack:       row.Stack,
			FirstSeen:   row.FirstSeen,
			LastSeen:    row.LastSeen,
			Count:       row.Count,
		})
	}
	return out, nil
}
