package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/infodancer/web/contact/handler"
	"github.com/infodancer/web/contact/submissions"
	"github.com/infodancer/web/contact/submissions/sqlite"
)

type recordingMailer struct {
	mu       sync.Mutex
	messages []handler.Message
	failNext bool
}

func (m *recordingMailer) Send(_ context.Context, msg handler.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failNext {
		m.failNext = false
		return errStub
	}
	m.messages = append(m.messages, msg)
	return nil
}

func (m *recordingMailer) all() []handler.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]handler.Message, len(m.messages))
	copy(out, m.messages)
	return out
}

type stubErr string

func (e stubErr) Error() string { return string(e) }

const errStub = stubErr("stub send failure")

func newHandler(t *testing.T) (*handler.Contact, *recordingMailer, *submissions.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "contact.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	mailer := &recordingMailer{}
	h := handler.New(store, mailer, "amy@example.org", "contact@example.org")
	return h, mailer, store
}

func post(h http.Handler, body url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHappyPath(t *testing.T) {
	h, mailer, store := newHandler(t)

	rec := post(h, url.Values{
		"name":    {"Ada Lovelace"},
		"email":   {"ada@example.org"},
		"subject": {"a question"},
		"message": {"please reply"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Thanks") {
		t.Errorf("body should contain success: %s", rec.Body.String())
	}

	h.Wait()

	msgs := mailer.all()
	if len(msgs) != 1 {
		t.Fatalf("messages sent: got %d want 1", len(msgs))
	}
	m := msgs[0]
	if m.From != "contact@example.org" {
		t.Errorf("From: got %q want contact@example.org", m.From)
	}
	if len(m.To) != 1 || m.To[0] != "amy@example.org" {
		t.Errorf("To: got %v want [amy@example.org]", m.To)
	}
	if len(h.To) != 1 || h.To[0] != "amy@example.org" {
		t.Errorf("h.To: got %v want [amy@example.org]", h.To)
	}
	if m.ReplyTo != "ada@example.org" {
		t.Errorf("ReplyTo: got %q want ada@example.org", m.ReplyTo)
	}
	if !strings.Contains(m.Subject, "a question") {
		t.Errorf("Subject lacks user subject: %q", m.Subject)
	}
	if !strings.Contains(m.Body, "please reply") {
		t.Errorf("Body lacks message: %q", m.Body)
	}

	// Submission should be marked sent.
	pending, err := store.Pending(context.Background(), 5, 10)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("Pending after success: got %d want 0", len(pending))
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h, _, _ := newHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/contact", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d want 405", rec.Code)
	}
}

func TestHoneypotSilentlyAccepts(t *testing.T) {
	h, mailer, store := newHandler(t)
	rec := post(h, url.Values{
		"name":    {"Bot"},
		"email":   {"bot@example.org"},
		"message": {"buy stuff"},
		"website": {"http://spam.example"}, // honeypot
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Thanks") {
		t.Errorf("body should still look successful: %s", rec.Body.String())
	}

	h.Wait()
	if len(mailer.all()) != 0 {
		t.Errorf("honeypot should suppress mail send; got %d", len(mailer.all()))
	}
	pending, _ := store.Pending(context.Background(), 5, 10)
	if len(pending) != 0 {
		t.Errorf("honeypot should suppress persistence; got %d", len(pending))
	}
}

func TestMissingFieldsRejected(t *testing.T) {
	h, mailer, _ := newHandler(t)
	cases := []url.Values{
		{"email": {"a@b.org"}, "message": {"hi"}},                       // no name
		{"name": {"Ada"}, "message": {"hi"}},                            // no email
		{"name": {"Ada"}, "email": {"a@b.org"}},                         // no message
		{"name": {"Ada"}, "email": {"not-an-email"}, "message": {"hi"}}, // bad email
	}
	for i, c := range cases {
		rec := post(h, c)
		if rec.Code != http.StatusOK {
			t.Errorf("case %d: status %d want 200", i, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Please fill") {
			t.Errorf("case %d: body should be invalid msg: %s", i, rec.Body.String())
		}
	}
	h.Wait()
	if len(mailer.all()) != 0 {
		t.Errorf("invalid submissions should not mail; got %d", len(mailer.all()))
	}
}

func TestOversizedMessageRejected(t *testing.T) {
	h, mailer, _ := newHandler(t)
	rec := post(h, url.Values{
		"name":    {"Ada"},
		"email":   {"ada@example.org"},
		"message": {strings.Repeat("x", h.MaxMessageLen+1)},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Please fill") {
		t.Errorf("body should be invalid msg: %s", rec.Body.String())
	}
	h.Wait()
	if len(mailer.all()) != 0 {
		t.Errorf("oversized submissions should not mail; got %d", len(mailer.all()))
	}
}

func TestSendFailureMarksFailed(t *testing.T) {
	h, mailer, store := newHandler(t)
	mailer.failNext = true

	rec := post(h, url.Values{
		"name":    {"Ada"},
		"email":   {"ada@example.org"},
		"message": {"please reply"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	h.Wait()

	pending, err := store.Pending(context.Background(), 5, 10)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending row after send failure; got %d", len(pending))
	}
}

func TestMultipleRecipients(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "contact.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	mailer := &recordingMailer{}
	h := handler.New(store, mailer, " amy@example.org , matthew@example.org ,, ", "contact@example.org")

	want := []string{"amy@example.org", "matthew@example.org"}
	if len(h.To) != len(want) {
		t.Fatalf("h.To: got %v want %v", h.To, want)
	}
	for i, addr := range want {
		if h.To[i] != addr {
			t.Errorf("h.To[%d]: got %q want %q", i, h.To[i], addr)
		}
	}

	rec := post(h, url.Values{
		"name":    {"Ada"},
		"email":   {"ada@example.org"},
		"message": {"hi"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	h.Wait()

	msgs := mailer.all()
	if len(msgs) != 1 {
		t.Fatalf("messages sent: got %d want 1", len(msgs))
	}
	if len(msgs[0].To) != len(want) {
		t.Fatalf("msg.To: got %v want %v", msgs[0].To, want)
	}
	for i, addr := range want {
		if msgs[0].To[i] != addr {
			t.Errorf("msg.To[%d]: got %q want %q", i, msgs[0].To[i], addr)
		}
	}
}

func TestParseAddrList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a@x.org", []string{"a@x.org"}},
		{"a@x.org,b@y.org", []string{"a@x.org", "b@y.org"}},
		{" a@x.org , b@y.org ", []string{"a@x.org", "b@y.org"}},
		{",,a@x.org,,", []string{"a@x.org"}},
	}
	for _, c := range cases {
		got := handler.ParseAddrList(c.in)
		if len(got) != len(c.want) {
			t.Errorf("ParseAddrList(%q): got %v want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseAddrList(%q)[%d]: got %q want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestHeaderInjectionScrubbed(t *testing.T) {
	h, mailer, _ := newHandler(t)
	rec := post(h, url.Values{
		"name":    {"Ada\r\nBcc: leak@example.org"},
		"email":   {"ada@example.org"},
		"subject": {"normal\r\nX-Injected: yes"},
		"message": {"hi"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	h.Wait()

	msgs := mailer.all()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message")
	}
	if strings.Contains(msgs[0].Subject, "\n") || strings.Contains(msgs[0].Subject, "\r") {
		t.Errorf("subject still contains line break: %q", msgs[0].Subject)
	}
	if strings.Contains(msgs[0].Body, "Bcc: leak") {
		// Body templates render the name; ensure no header bleed-through happens.
		// Header injection would only matter if the body string is reused as headers,
		// but the From: header itself comes from h.From, not the template, so this
		// is informational rather than a fail.
		t.Logf("note: body contains user-provided string with what looks like a header — expected since body is plain text")
	}
}
