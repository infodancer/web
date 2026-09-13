package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/infodancer/logging/httplog"
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

func validSubmission() url.Values {
	return url.Values{
		"name":    {"Ada Lovelace"},
		"email":   {"ada@example.org"},
		"message": {"please reply"},
	}
}

// assertNothingHappened checks a submission was neither stored nor mailed.
func assertNothingHappened(t *testing.T, h interface{ Wait() }, mailer *recordingMailer, store *memStore) {
	t.Helper()
	h.Wait()
	if n := len(mailer.all()); n != 0 {
		t.Errorf("mail sent: got %d, want 0", n)
	}
	if pending, _ := store.Pending(context.Background(), 5, 10); len(pending) != 0 {
		t.Errorf("submissions stored: got %d, want 0", len(pending))
	}
}

func TestGuardIsAskedAboutTheParsedContactForm(t *testing.T) {
	h, _, _ := newHandler(t)
	g := &fakeGuard{verdict: antibot.Verdict{Allowed: true}}
	h.Guard = g

	post(h, validSubmission())
	h.Wait()

	if len(g.forms) != 1 || g.forms[0] != "contact" {
		t.Fatalf("guard asked about forms %v, want [contact]", g.forms)
	}
	if !g.parsed[0] {
		t.Fatal("guard was called before the form was parsed")
	}
}

func TestGuardFormNameCanBeChanged(t *testing.T) {
	h, _, _ := newHandler(t)
	g := &fakeGuard{verdict: antibot.Verdict{Allowed: true}}
	h.Guard = g
	h.Form = "press-inquiry"

	post(h, validSubmission())
	h.Wait()

	if len(g.forms) != 1 || g.forms[0] != "press-inquiry" {
		t.Fatalf("guard asked about forms %v, want [press-inquiry]", g.forms)
	}
}

func TestGuardRejectionIsRenderedAndNothingIsSent(t *testing.T) {
	for _, reason := range []antibot.Reason{
		antibot.ReasonRateLimited, antibot.ReasonTokenMissing, antibot.ReasonTokenInvalid,
		antibot.ReasonTooFast, antibot.ReasonTokenExpired,
	} {
		t.Run(string(reason), func(t *testing.T) {
			h, mailer, store := newHandler(t)
			h.Guard = &fakeGuard{verdict: antibot.Verdict{Reason: reason}}

			rec := post(h, validSubmission())

			if got := rec.Body.String(); got != h.MsgRejected {
				t.Errorf("body = %q, want MsgRejected", got)
			}
			assertNothingHappened(t, h, mailer, store)
		})
	}
}

// An address that cannot receive mail is the visitor's typo as often as a
// bot's, so it gets the message that asks them to check the form.
func TestGuardEmailRejectionAsksForAValidAddress(t *testing.T) {
	for _, reason := range []antibot.Reason{antibot.ReasonInvalidEmail, antibot.ReasonNoMX} {
		t.Run(string(reason), func(t *testing.T) {
			h, mailer, store := newHandler(t)
			h.Guard = &fakeGuard{verdict: antibot.Verdict{Reason: reason}}

			rec := post(h, validSubmission())

			if got := rec.Body.String(); got != h.MsgInvalid {
				t.Errorf("body = %q, want MsgInvalid", got)
			}
			assertNothingHappened(t, h, mailer, store)
		})
	}
}

// A honeypot trip must look exactly like success, or the bot learns which
// field gave it away.
func TestGuardSilentVerdictLooksLikeSuccess(t *testing.T) {
	h, mailer, store := newHandler(t)
	h.Guard = &fakeGuard{verdict: antibot.Verdict{Silent: true, Reason: antibot.ReasonHoneypot}}

	rec := post(h, validSubmission())

	if got := rec.Body.String(); got != h.MsgSuccess {
		t.Errorf("body = %q, want MsgSuccess", got)
	}
	assertNothingHappened(t, h, mailer, store)
}

// The guard fails closed when its store is down; the handler must not treat
// that as permission.
func TestGuardErrorFailsClosed(t *testing.T) {
	h, mailer, store := newHandler(t)
	h.Guard = &fakeGuard{verdict: antibot.Verdict{Reason: antibot.ReasonUnavailable}, err: errors.New("db down")}

	rec := post(h, validSubmission())

	if got := rec.Body.String(); got != h.MsgError {
		t.Errorf("body = %q, want MsgError", got)
	}
	assertNothingHappened(t, h, mailer, store)
}

// With a guard in place the honeypot is its job. A handler that also checked
// the field itself would accept silently without the guard ever counting it.
func TestGuardOwnsTheHoneypot(t *testing.T) {
	h, _, _ := newHandler(t)
	g := &fakeGuard{verdict: antibot.Verdict{Silent: true, Reason: antibot.ReasonHoneypot}}
	h.Guard = g

	body := validSubmission()
	body.Set("website", "http://spam.example")
	post(h, body)
	h.Wait()

	if len(g.forms) != 1 {
		t.Fatalf("guard calls = %d, want 1", len(g.forms))
	}
}

func postFrom(h http.Handler, remote, xff string, body url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remote
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The address stored and mailed with a submission used to be the leftmost
// X-Forwarded-For entry, which the sender writes. From an untrusted peer the
// header is ignored.
func TestRecordedIPIgnoresForwardedForFromAnUntrustedPeer(t *testing.T) {
	h, mailer, store := newHandler(t)

	postFrom(h, "198.51.100.9:4000", "203.0.113.66", validSubmission())
	h.Wait()

	if len(store.rows) != 1 {
		t.Fatalf("stored %d submissions, want 1", len(store.rows))
	}
	if got := store.rows[0].IP; got != "198.51.100.9" {
		t.Errorf("stored IP = %q, want the peer 198.51.100.9", got)
	}
	if msgs := mailer.all(); len(msgs) != 1 || strings.Contains(msgs[0].Body, "203.0.113.66") {
		t.Errorf("the forged address reached the notification: %+v", msgs)
	}
}

// Behind trusted proxies the visitor is the rightmost hop that is not one of
// them -- not the leftmost entry, which a client can prepend.
func TestRecordedIPIsTheVisitorBehindTrustedProxies(t *testing.T) {
	h, _, store := newHandler(t)
	h.Proxies = httplog.NewProxies("10.0.0.0/8")

	postFrom(h, "10.0.0.2:4000", "203.0.113.66, 198.51.100.20, 10.0.0.3", validSubmission())
	h.Wait()

	if len(store.rows) != 1 {
		t.Fatalf("stored %d submissions, want 1", len(store.rows))
	}
	if got := store.rows[0].IP; got != "198.51.100.20" {
		t.Errorf("stored IP = %q, want the visitor 198.51.100.20", got)
	}
}
