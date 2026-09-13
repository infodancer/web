package antibot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// Signed-in abuse -- one account posting from many addresses -- is what an IP
// limit misses and the account limit is for.
func TestAccountLimitHoldsAcrossIPs(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) {
		cfg.IPLimit = NoLimit
		cfg.AccountLimit = Limit{Max: 2, Window: 24 * time.Hour}
	})

	for i := 1; i <= 2; i++ {
		remote := fmt.Sprintf("203.0.113.%d:5555", i)
		if v, _ := submit(t, g, c, "join", remote, nil, Account("42")); !v.Allowed {
			t.Fatalf("submission %d rejected: %+v", i, v)
		}
	}
	if v, _ := submit(t, g, c, "join", "203.0.113.99:5555", nil, Account("42")); v.Allowed || v.Reason != ReasonRateLimited {
		t.Fatalf("third submission for one account from a new IP: got %+v, want %q", v, ReasonRateLimited)
	}
	if v, _ := submit(t, g, c, "join", "203.0.113.99:5555", nil, Account("43")); !v.Allowed {
		t.Fatalf("another account shares the first one's budget: got %+v", v)
	}
}

// An account id and an email with the same text are different keys.
func TestAccountKeyIsItsOwnKind(t *testing.T) {
	store := newFakeStore()
	g, c := testGuard(t, store, func(cfg *Config) { cfg.IPLimit = NoLimit })

	if _, err := submit(t, g, c, "join", "203.0.113.7:5555", nil, Account("victim@example.com")); err != nil {
		t.Fatal(err)
	}
	if len(store.params) != 1 || store.params[0].KeyKind != kindAccount {
		t.Fatalf("counter writes %+v, want one of kind %q", store.params, kindAccount)
	}
	if bytes.Equal(g.keyHash(kindAccount, "victim@example.com"), g.keyHash(kindEmail, "victim@example.com")) {
		t.Fatal("an account id hashes to the same key as the equal email")
	}
}

// An empty id is a caller bug. Counting it would put every such caller in one
// shared bucket; skipping it would silently drop the limit.
func TestEmptyAccountIsAnError(t *testing.T) {
	store := newFakeStore()
	g, c := testGuard(t, store, nil)

	v, err := submit(t, g, c, "join", "203.0.113.7:5555", nil, Account(""))
	if !errors.Is(err, ErrEmptyAccount) {
		t.Fatalf("err = %v, want ErrEmptyAccount", err)
	}
	if v.Allowed {
		t.Fatalf("got %+v, want a rejection", v)
	}
	r := httptest.NewRequest(http.MethodPost, "/join", nil)
	if _, err := g.Limit(context.Background(), r, "join", Account("")); !errors.Is(err, ErrEmptyAccount) {
		t.Fatalf("Limit: err = %v, want ErrEmptyAccount", err)
	}
	if store.calls != 0 {
		t.Errorf("an empty account wrote %d counter(s); want none", store.calls)
	}
}

func TestNewRejectsAnAccountLimitWithNoWindow(t *testing.T) {
	cfg := Config{Secret: testSecret, AccountLimit: Limit{Max: 3}}
	if _, err := newGuard(cfg, newFakeStore()); err == nil {
		t.Fatal("newGuard accepted an account limit with no window")
	}
}

// Limit is for forms behind a sign-in, where the timing token and honeypot add
// nothing: it counts, and checks nothing else. It needs no parsed form.
func TestLimitSkipsTheTokenAndHoneypot(t *testing.T) {
	store := newFakeStore()
	g, _ := testGuard(t, store, nil)
	r := httptest.NewRequest(http.MethodPost, "/join", nil)
	r.RemoteAddr = "203.0.113.7:5555"

	v, err := g.Limit(context.Background(), r, "join", Account("42"))
	if err != nil || !v.Allowed {
		t.Fatalf("got %+v, %v; want allowed", v, err)
	}
	if len(store.params) != 2 {
		t.Fatalf("got %d counter writes, want 2 (ip and account)", len(store.params))
	}

	// A filled honeypot is not Limit's business either.
	r = request{remote: "203.0.113.8:5555", fields: url.Values{defaultHoneypotField: {"x"}}}.build(t)
	if v, err := g.Limit(context.Background(), r, "join"); err != nil || !v.Allowed {
		t.Fatalf("honeypot field: got %+v, %v; want allowed", v, err)
	}
}

func TestLimitEnforcesTheIPLimit(t *testing.T) {
	g, _ := testGuard(t, newFakeStore(), func(cfg *Config) { cfg.IPLimit = Limit{Max: 2, Window: time.Hour} })
	limit := func() Verdict {
		r := httptest.NewRequest(http.MethodPost, "/add", nil)
		r.RemoteAddr = "203.0.113.7:5555"
		v, err := g.Limit(context.Background(), r, "add")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	for i := 1; i <= 2; i++ {
		if v := limit(); !v.Allowed {
			t.Fatalf("request %d rejected: %+v", i, v)
		}
	}
	if v := limit(); v.Allowed || v.Reason != ReasonRateLimited {
		t.Fatalf("request 3: got %+v, want %q", v, ReasonRateLimited)
	}
}

func TestLimitStoreErrorFailsClosed(t *testing.T) {
	store := newFakeStore()
	store.err = errStore
	g, _ := testGuard(t, store, nil)
	r := httptest.NewRequest(http.MethodPost, "/join", nil)

	v, err := g.Limit(context.Background(), r, "join", Account("42"))
	if !errors.Is(err, errStore) {
		t.Fatalf("err = %v, want the store error wrapped", err)
	}
	if v.Allowed || v.Reason != ReasonUnavailable {
		t.Fatalf("got %+v, want rejected with %q", v, ReasonUnavailable)
	}
}

func TestAccountKeyStoresNoRawIdentifier(t *testing.T) {
	store := newFakeStore()
	g, _ := testGuard(t, store, func(cfg *Config) { cfg.IPLimit = NoLimit })
	const id = "user-8675309"
	r := httptest.NewRequest(http.MethodPost, "/join", nil)

	if _, err := g.Limit(context.Background(), r, "join", Account(id)); err != nil {
		t.Fatal(err)
	}
	for _, p := range store.params {
		if len(p.KeyHash) != 32 || bytes.Contains(p.KeyHash, []byte(id)) {
			t.Errorf("%s key %x is not an opaque 32-byte HMAC of %q", p.KeyKind, p.KeyHash, id)
		}
	}
}
