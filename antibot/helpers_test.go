package antibot

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/infodancer/web/antibot/db"
)

// t0 is the fixed instant every unit test starts from.
var t0 = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

var testSecret = []byte("0123456789abcdef0123456789abcdef")

// clock is a settable time source for Config.Now.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeStore counts hits in memory with the same semantics as the SQL upsert:
// one counter per (form, key kind, key hash, window start).
type fakeStore struct {
	mu     sync.Mutex
	counts map[string]int32
	calls  int
	sweeps int
	params []db.HitParams
	err    error
}

func newFakeStore() *fakeStore { return &fakeStore{counts: map[string]int32{}} }

func (f *fakeStore) Hit(_ context.Context, p db.HitParams) (int32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.params = append(f.params, p)
	if f.err != nil {
		return 0, f.err
	}
	k := p.Form + "\x00" + p.KeyKind + "\x00" + string(p.KeyHash) + "\x00" + p.WindowStart.UTC().String()
	f.counts[k]++
	return f.counts[k], nil
}

func (f *fakeStore) Sweep(_ context.Context, _ time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweeps++
	return 0, f.err
}

// Sweeps reports how many times Sweep has been called.
func (f *fakeStore) Sweeps() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sweeps
}

// mxResolver is a fake MX lookup that counts calls.
type mxResolver struct {
	mu      sync.Mutex
	calls   int
	records []*net.MX
	err     error
}

func (m *mxResolver) LookupMX(_ context.Context, _ string) ([]*net.MX, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return m.records, m.err
}

func (m *mxResolver) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func goodMX() *mxResolver {
	return &mxResolver{records: []*net.MX{{Host: "mx.example.com.", Pref: 10}}}
}

// testGuard builds a Guard over a fake store with a fixed clock, a discard
// logger and a working MX resolver. edit adjusts the config before New.
func testGuard(t *testing.T, store *fakeStore, edit func(*Config)) (*Guard, *clock) {
	t.Helper()
	c := &clock{now: t0}
	cfg := Config{
		Secret:   testSecret,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:      c.Now,
		LookupMX: goodMX().LookupMX,
	}
	if edit != nil {
		edit(&cfg)
	}
	g, err := newGuard(cfg, store)
	if err != nil {
		t.Fatalf("newGuard: %v", err)
	}
	return g, c
}

// request is a parsed POST as a handler would hand it to Check.
type request struct {
	remote  string
	headers map[string]string
	fields  url.Values
}

func (rq request) build(t *testing.T) *http.Request {
	t.Helper()
	body := rq.fields.Encode()
	r := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rq.remote != "" {
		r.RemoteAddr = rq.remote
	}
	for k, v := range rq.headers {
		r.Header.Set(k, v)
	}
	if err := r.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	return r
}

// submit sends a request carrying a freshly issued, already-aged token for
// form, as a real browser would after filling the form in.
func submit(t *testing.T, g *Guard, c *clock, form, remote string, headers map[string]string, opts ...CheckOption) (Verdict, error) {
	t.Helper()
	tok := g.Token(form)
	c.Advance(10 * time.Second)
	r := request{remote: remote, headers: headers, fields: url.Values{defaultTokenField: {tok}}}.build(t)
	return g.Check(context.Background(), r, form, opts...)
}

var errStore = errors.New("store unavailable")

// logBuffer captures JSON log records for assertions.
func logBuffer() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
}
