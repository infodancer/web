package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/infodancer/web/newsletter/listmonk"
)

// fakeStore is an in-memory Store for handler unit tests.
type fakeStore struct {
	insertErr  error
	isNew      bool
	syncErr    error
	syncedMail string
}

func (f *fakeStore) Insert(string) (bool, error) { return f.isNew, f.insertErr }

func (f *fakeStore) MarkSynced(email string) error {
	f.syncedMail = email
	return f.syncErr
}

type ridKey struct{}

// newTestHandler builds a Subscribe wired to a JSON logger writing into buf, so
// a test can assert the level, message, and structured fields of a log line.
// ListUUID is empty so the Listmonk client is never called.
func newTestHandler(buf *bytes.Buffer, store Store) *Subscribe {
	h := New(store, nil, "")
	h.Logger = slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h.RequestID = func(ctx context.Context) string {
		id, _ := ctx.Value(ridKey{}).(string)
		return id
	}
	return h
}

// subscribeRequest builds a urlencoded POST /subscribe carrying the request id
// in context so the handler's scoped logger can attach it.
func subscribeRequest(email, reqID string) *http.Request {
	form := url.Values{"email": {email}}
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req.WithContext(context.WithValue(req.Context(), ridKey{}, reqID))
}

// firstLogEntry decodes the first JSON log line in buf.
func firstLogEntry(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	if buf.Len() == 0 {
		t.Fatal("expected a log line, got none")
	}
	line := bytes.SplitN(buf.Bytes(), []byte("\n"), 2)[0]
	var entry map[string]any
	if err := json.Unmarshal(line, &entry); err != nil {
		t.Fatalf("log line not JSON: %v (%s)", err, line)
	}
	return entry
}

// A database insert failure logs one structured ERROR with the request id and
// err, and returns the generic error fragment to the client.
func TestServeHTTP_InsertErrorLogsError(t *testing.T) {
	var buf bytes.Buffer
	h := newTestHandler(&buf, &fakeStore{insertErr: errors.New("db down")})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, subscribeRequest("reader@example.com", "rid-1"))

	if got := rec.Body.String(); got != h.MsgError {
		t.Errorf("body = %q, want MsgError", got)
	}
	entry := firstLogEntry(t, &buf)
	if entry["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", entry["level"])
	}
	if entry["msg"] != "insert subscriber" {
		t.Errorf("msg = %v, want %q", entry["msg"], "insert subscriber")
	}
	if entry["request_id"] != "rid-1" {
		t.Errorf("request_id = %v, want rid-1", entry["request_id"])
	}
	if entry["err"] == nil {
		t.Error("err field missing")
	}
}

// An MX validation rejection is user input, not a fault: it logs at INFO and
// returns the invalid-domain fragment.
func TestServeHTTP_MXRejectionLogsInfo(t *testing.T) {
	var buf bytes.Buffer
	h := newTestHandler(&buf, &fakeStore{isNew: true})

	rec := httptest.NewRecorder()
	// A malformed address (no domain part) fails ValidateMX before any DNS
	// lookup, so the test is deterministic and needs no network.
	h.ServeHTTP(rec, subscribeRequest("not-an-email", "rid-2"))

	if got := rec.Body.String(); got != h.MsgInvalidDomain {
		t.Errorf("body = %q, want MsgInvalidDomain", got)
	}
	entry := firstLogEntry(t, &buf)
	if entry["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", entry["level"])
	}
	if entry["msg"] != "mx validation rejected" {
		t.Errorf("msg = %v, want %q", entry["msg"], "mx validation rejected")
	}
	if entry["request_id"] != "rid-2" {
		t.Errorf("request_id = %v, want rid-2", entry["request_id"])
	}
}

// A Listmonk sync failure is swallowed (the subscribe still succeeds) but logs
// at WARN so an operator can see the row went unsynced. The Listmonk client is
// pointed at a closed server so Subscribe returns an error.
func TestServeHTTP_ListmonkFailureLogsWarn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	store := &fakeStore{isNew: true}
	h := New(store, listmonk.NewClient(srv.URL), "list-uuid")
	h.Logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h.RequestID = func(ctx context.Context) string {
		id, _ := ctx.Value(ridKey{}).(string)
		return id
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, subscribeRequest("reader@example.com", "rid-3"))

	// The subscribe still succeeds for a new email despite the sync failure.
	if got := rec.Body.String(); got != h.MsgSuccess {
		t.Errorf("body = %q, want MsgSuccess (sync failure must not fail the request)", got)
	}
	if store.syncedMail != "" {
		t.Errorf("MarkSynced called despite sync failure: %q", store.syncedMail)
	}
	entry := firstLogEntry(t, &buf)
	if entry["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", entry["level"])
	}
	if msg, _ := entry["msg"].(string); !strings.HasPrefix(msg, "listmonk sync failed") {
		t.Errorf("msg = %v, want a \"listmonk sync failed\" prefix", entry["msg"])
	}
	if entry["request_id"] != "rid-3" {
		t.Errorf("request_id = %v, want rid-3", entry["request_id"])
	}
}

// An empty email writes the empty fragment and logs nothing.
func TestServeHTTP_EmptyEmailNoLog(t *testing.T) {
	var buf bytes.Buffer
	h := newTestHandler(&buf, &fakeStore{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, subscribeRequest("", "rid-4"))
	if got := rec.Body.String(); got != h.MsgEmpty {
		t.Errorf("body = %q, want MsgEmpty", got)
	}
	if buf.Len() != 0 {
		t.Errorf("empty email logged a line: %s", buf.String())
	}
}

// A non-POST request is rejected with 405.
func TestServeHTTP_MethodNotAllowed(t *testing.T) {
	h := New(&fakeStore{}, nil, "")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/subscribe", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
