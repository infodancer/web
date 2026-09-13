package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/infodancer/web/antibot"
)

// fakeGuard returns a fixed verdict and records what it was asked.
type fakeGuard struct {
	mu      sync.Mutex
	verdict antibot.Verdict
	err     error
	forms   []string
	parsed  []bool
}

func (g *fakeGuard) Check(_ context.Context, r *http.Request, form string, _ ...antibot.CheckOption) (antibot.Verdict, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.forms = append(g.forms, form)
	g.parsed = append(g.parsed, r.PostForm != nil)
	return g.verdict, g.err
}

// countingStore records how many inserts reached it.
type countingStore struct {
	fakeStore
	inserts int
}

func (s *countingStore) Insert(email string) (bool, error) {
	s.inserts++
	return s.fakeStore.Insert(email)
}

func guardedHandler(t *testing.T, g Guard) (*Subscribe, *countingStore, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	store := &countingStore{fakeStore: fakeStore{isNew: true}}
	h := newTestHandler(&buf, store)
	h.Guard = g
	return h, store, &buf
}

func TestGuardIsAskedAboutTheParsedNewsletterForm(t *testing.T) {
	g := &fakeGuard{verdict: antibot.Verdict{Allowed: true}}
	h, _, _ := guardedHandler(t, g)

	h.ServeHTTP(httptest.NewRecorder(), subscribeRequest("reader@example.com", "rid"))

	if len(g.forms) != 1 || g.forms[0] != "newsletter" {
		t.Fatalf("guard asked about forms %v, want [newsletter]", g.forms)
	}
	if !g.parsed[0] {
		t.Fatal("guard was called before the form was parsed")
	}
}

func TestGuardRejectionIsRenderedAndNothingIsStored(t *testing.T) {
	for _, reason := range []antibot.Reason{
		antibot.ReasonRateLimited, antibot.ReasonTokenMissing, antibot.ReasonTokenInvalid,
		antibot.ReasonTooFast, antibot.ReasonTokenExpired,
	} {
		t.Run(string(reason), func(t *testing.T) {
			h, store, _ := guardedHandler(t, &fakeGuard{verdict: antibot.Verdict{Reason: reason}})
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, subscribeRequest("reader@example.com", "rid"))

			if got := rec.Body.String(); got != h.MsgRejected {
				t.Errorf("body = %q, want MsgRejected", got)
			}
			if store.inserts != 0 {
				t.Errorf("inserts = %d, want 0", store.inserts)
			}
		})
	}
}

func TestGuardEmailRejectionAsksToCheckTheAddress(t *testing.T) {
	for _, reason := range []antibot.Reason{antibot.ReasonInvalidEmail, antibot.ReasonNoMX} {
		t.Run(string(reason), func(t *testing.T) {
			h, store, _ := guardedHandler(t, &fakeGuard{verdict: antibot.Verdict{Reason: reason}})
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, subscribeRequest("reader@example.com", "rid"))

			if got := rec.Body.String(); got != h.MsgInvalidDomain {
				t.Errorf("body = %q, want MsgInvalidDomain", got)
			}
			if store.inserts != 0 {
				t.Errorf("inserts = %d, want 0", store.inserts)
			}
		})
	}
}

func TestGuardSilentVerdictLooksLikeSuccess(t *testing.T) {
	h, store, _ := guardedHandler(t, &fakeGuard{verdict: antibot.Verdict{Silent: true, Reason: antibot.ReasonHoneypot}})
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, subscribeRequest("reader@example.com", "rid"))

	if got := rec.Body.String(); got != h.MsgSuccess {
		t.Errorf("body = %q, want MsgSuccess", got)
	}
	if store.inserts != 0 {
		t.Errorf("inserts = %d, want 0", store.inserts)
	}
}

func TestGuardErrorFailsClosed(t *testing.T) {
	h, store, buf := guardedHandler(t, &fakeGuard{verdict: antibot.Verdict{Reason: antibot.ReasonUnavailable}, err: errors.New("db down")})
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, subscribeRequest("reader@example.com", "rid-9"))

	if got := rec.Body.String(); got != h.MsgError {
		t.Errorf("body = %q, want MsgError", got)
	}
	if store.inserts != 0 {
		t.Errorf("inserts = %d, want 0", store.inserts)
	}
	entry := firstLogEntry(t, buf)
	if entry["level"] != "ERROR" || entry["request_id"] != "rid-9" || entry["err"] == nil {
		t.Errorf("log = %v, want ERROR with request_id and err", entry)
	}
}

// With a guard in place, the guard checks the mail domain: the handler must not
// also run the old lookup, which has no rate limit in front of it and would
// resolve any domain a bot names.
func TestGuardReplacesTheOldMXCheck(t *testing.T) {
	h, store, _ := guardedHandler(t, &fakeGuard{verdict: antibot.Verdict{Allowed: true}})
	rec := httptest.NewRecorder()

	// Rejected by the old check before any DNS lookup; the guard allowed it.
	h.ServeHTTP(rec, subscribeRequest("not-an-email", "rid"))

	if got := rec.Body.String(); got != h.MsgSuccess {
		t.Errorf("body = %q, want MsgSuccess", got)
	}
	if store.inserts != 1 {
		t.Errorf("inserts = %d, want 1", store.inserts)
	}
}
