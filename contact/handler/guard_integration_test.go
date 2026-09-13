package handler_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/infodancer/web/antibot"
	"github.com/infodancer/web/pgtest"
)

// The resume checker's case, through the real guard and a real database: one
// address submitting from many IPs is still one sender. It also proves the
// handler hands the guard the submitted email, which a fake cannot observe.
func TestRealGuardLimitsOneAddressAcrossIPs(t *testing.T) {
	pool := pgtest.Pool(t, pgtest.WithMigrate(antibot.Migrate))
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	g, err := antibot.New(antibot.Config{
		DB:         pool,
		Secret:     []byte("0123456789abcdef0123456789abcdef"),
		EmailLimit: antibot.Limit{Max: 1, Window: time.Hour},
		LookupMX: func(context.Context, string) ([]*net.MX, error) {
			return []*net.MX{{Host: "mx.example.org.", Pref: 10}}, nil
		},
		Now:    func() time.Time { return now },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("antibot.New: %v", err)
	}
	h, mailer, store := newHandler(t)
	h.Guard = g

	tok := g.Token("contact")
	now = now.Add(time.Minute)
	body := validSubmission()
	body.Set("antibot_token", tok)

	if rec := postFrom(h, "198.51.100.1:4000", "", body); rec.Body.String() != h.MsgSuccess {
		t.Fatalf("first submission: body = %q, want MsgSuccess", rec.Body.String())
	}
	if rec := postFrom(h, "198.51.100.2:4000", "", body); rec.Body.String() != h.MsgRejected {
		t.Fatalf("same address from a second IP: body = %q, want MsgRejected", rec.Body.String())
	}
	h.Wait()

	if len(store.rows) != 1 {
		t.Errorf("stored %d submissions, want 1", len(store.rows))
	}
	if n := len(mailer.all()); n != 1 {
		t.Errorf("sent %d messages, want 1", n)
	}
}

// Without the token a real guard rejects the submission, so a site that mounts
// the guard but forgets the page script fails loudly in testing.
func TestRealGuardRejectsASubmissionWithNoToken(t *testing.T) {
	pool := pgtest.Pool(t, pgtest.WithMigrate(antibot.Migrate))
	g, err := antibot.New(antibot.Config{
		DB:     pool,
		Secret: []byte("0123456789abcdef0123456789abcdef"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("antibot.New: %v", err)
	}
	h, mailer, store := newHandler(t)
	h.Guard = g

	rec := postFrom(h, "198.51.100.1:4000", "", validSubmission())

	if rec.Code != http.StatusOK || rec.Body.String() != h.MsgRejected {
		t.Fatalf("status %d body %q, want 200 MsgRejected", rec.Code, rec.Body.String())
	}
	assertNothingHappened(t, h, mailer, store)
}
