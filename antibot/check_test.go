package antibot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/infodancer/logging/httplog"
)

func TestNewRejectsAWeakSecret(t *testing.T) {
	for _, secret := range [][]byte{nil, []byte("short"), make([]byte, minSecretLen-1)} {
		if _, err := newGuard(Config{Secret: secret}, newFakeStore()); !errors.Is(err, ErrWeakSecret) {
			t.Errorf("secret of %d bytes: err = %v, want ErrWeakSecret", len(secret), err)
		}
	}
}

func TestNewRequiresADatabase(t *testing.T) {
	if _, err := New(Config{Secret: testSecret}); err == nil {
		t.Fatal("New accepted a config with no database")
	}
}

// Check reads the form the handler already parsed; it never touches the body.
// Called on an unparsed request, that is a programming error, not a bot.
func TestCheckOnAnUnparsedRequestIsAnError(t *testing.T) {
	g, _ := testGuard(t, newFakeStore(), nil)
	r := httptest.NewRequest(http.MethodPost, "/submit", nil)

	if _, err := g.Check(context.Background(), r, "contact"); !errors.Is(err, ErrFormNotParsed) {
		t.Fatalf("err = %v, want ErrFormNotParsed", err)
	}
}

// A filled honeypot is answered with silence -- the caller renders a fake
// success -- and costs nothing: no token check, no counter write.
func TestHoneypotIsSilentAndWritesNothing(t *testing.T) {
	store := newFakeStore()
	g, _ := testGuard(t, store, nil)
	r := request{remote: "203.0.113.7:5555", fields: url.Values{defaultHoneypotField: {"http://spam.example"}}}.build(t)

	v, err := g.Check(context.Background(), r, "contact")
	if err != nil {
		t.Fatal(err)
	}
	if v.Allowed || !v.Silent || v.Reason != ReasonHoneypot {
		t.Fatalf("got %+v, want a silent honeypot rejection", v)
	}
	if store.calls != 0 {
		t.Errorf("honeypot trip wrote %d counter(s); want none", store.calls)
	}
}

// A bot with no valid token is turned away before it costs a database write.
func TestTokenIsCheckedBeforeAnythingIsCounted(t *testing.T) {
	store := newFakeStore()
	g, _ := testGuard(t, store, nil)
	r := request{remote: "203.0.113.7:5555", fields: url.Values{}}.build(t)

	if v, _ := g.Check(context.Background(), r, "contact"); v.Allowed {
		t.Fatal("a request with no token was allowed")
	}
	if store.calls != 0 {
		t.Errorf("a tokenless request wrote %d counter(s); want none", store.calls)
	}
}

func TestIPLimit(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) { cfg.IPLimit = Limit{Max: 3, Window: time.Hour} })

	for i := 1; i <= 3; i++ {
		if v, err := submit(t, g, c, "contact", "203.0.113.7:5555", nil); err != nil || !v.Allowed {
			t.Fatalf("submission %d: got %+v, %v; want allowed", i, v, err)
		}
	}
	if v, _ := submit(t, g, c, "contact", "203.0.113.7:5555", nil); v.Allowed || v.Reason != ReasonRateLimited {
		t.Fatalf("submission 4: got %+v, want %q", v, ReasonRateLimited)
	}
}

func TestLimitRollsOverWithTheWindow(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) { cfg.IPLimit = Limit{Max: 1, Window: time.Hour} })

	if v, _ := submit(t, g, c, "contact", "203.0.113.7:5555", nil); !v.Allowed {
		t.Fatalf("first submission rejected: %+v", v)
	}
	if v, _ := submit(t, g, c, "contact", "203.0.113.7:5555", nil); v.Allowed {
		t.Fatal("second submission in the same window was allowed")
	}
	c.Advance(time.Hour)
	if v, _ := submit(t, g, c, "contact", "203.0.113.7:5555", nil); !v.Allowed {
		t.Fatalf("submission in the next window rejected: %+v", v)
	}
}

func TestEachFormHasItsOwnBudget(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) { cfg.IPLimit = Limit{Max: 1, Window: time.Hour} })

	if v, _ := submit(t, g, c, "contact", "203.0.113.7:5555", nil); !v.Allowed {
		t.Fatalf("contact rejected: %+v", v)
	}
	if v, _ := submit(t, g, c, "newsletter", "203.0.113.7:5555", nil); !v.Allowed {
		t.Fatalf("newsletter rejected after a contact submission: %+v", v)
	}
}

func TestNoLimitDisablesTheIPLimit(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) { cfg.IPLimit = NoLimit })

	for i := 0; i < 50; i++ {
		if v, _ := submit(t, g, c, "contact", "203.0.113.7:5555", nil); !v.Allowed {
			t.Fatalf("submission %d rejected with the IP limit disabled: %+v", i, v)
		}
	}
}

// X-Forwarded-For from a peer we have no reason to trust is ignored, so a
// client cannot buy a fresh bucket per request by rotating the header.
func TestLimitIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) {
		cfg.IPLimit = Limit{Max: 1, Window: time.Hour}
		cfg.Proxies = httplog.NewProxies("10.0.0.0/8")
	})

	if v, _ := submit(t, g, c, "contact", "203.0.113.7:5555", map[string]string{"X-Forwarded-For": "198.51.100.1"}); !v.Allowed {
		t.Fatalf("first submission rejected: %+v", v)
	}
	if v, _ := submit(t, g, c, "contact", "203.0.113.7:5555", map[string]string{"X-Forwarded-For": "198.51.100.2"}); v.Allowed {
		t.Fatal("rotating X-Forwarded-For from an untrusted peer bought a fresh bucket")
	}
}

// Behind the real chain (visitor -> Cloudflare -> Traefik) each visitor has
// their own budget, and a hop the visitor prepended does not change the key.
func TestLimitKeysTheVisitorBehindTrustedProxies(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) {
		cfg.IPLimit = Limit{Max: 1, Window: time.Hour}
		cfg.Proxies = httplog.NewProxies("10.0.0.0/8", "172.16.0.1")
	})
	via := func(visitor string) map[string]string {
		return map[string]string{"X-Forwarded-For": visitor + ", 172.16.0.1"}
	}

	if v, _ := submit(t, g, c, "contact", "10.0.0.1:443", via("198.51.100.9")); !v.Allowed {
		t.Fatalf("first visitor rejected: %+v", v)
	}
	if v, _ := submit(t, g, c, "contact", "10.0.0.1:443", via("198.51.100.10")); !v.Allowed {
		t.Fatalf("second visitor shared the first visitor's bucket: %+v", v)
	}
	if v, _ := submit(t, g, c, "contact", "10.0.0.1:443", via("1.1.1.1, 198.51.100.9")); v.Allowed {
		t.Fatal("a prepended hop bought the first visitor a fresh bucket")
	}
}

// One IPv6 subscriber usually holds a whole /64, so keying on the full address
// hands them 2^64 buckets. The limiter keys on the /64.
func TestIPv6IsKeyedOnTheSlash64(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) { cfg.IPLimit = Limit{Max: 1, Window: time.Hour} })

	if v, _ := submit(t, g, c, "contact", "[2001:db8:1:2::1]:443", nil); !v.Allowed {
		t.Fatalf("first address rejected: %+v", v)
	}
	if v, _ := submit(t, g, c, "contact", "[2001:db8:1:2:ffff::9]:443", nil); v.Allowed {
		t.Fatal("another address in the same /64 bought a fresh bucket")
	}
	if v, _ := submit(t, g, c, "contact", "[2001:db8:1:3::1]:443", nil); !v.Allowed {
		t.Fatalf("an address in a different /64 was rejected: %+v", v)
	}
}

// The resume checker's abuse case: one address, many IPs. The email key catches
// it where the IP key cannot, and case does not split the budget.
func TestEmailLimitHoldsAcrossIPs(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) {
		cfg.IPLimit = NoLimit
		cfg.EmailLimit = Limit{Max: 2, Window: 24 * time.Hour}
	})

	for i, addr := range []string{"victim@example.com", "Victim@Example.COM"} {
		remote := fmt.Sprintf("203.0.113.%d:5555", i+1)
		if v, _ := submit(t, g, c, "resume", remote, nil, Email(addr)); !v.Allowed {
			t.Fatalf("submission %d rejected: %+v", i+1, v)
		}
	}
	if v, _ := submit(t, g, c, "resume", "203.0.113.99:5555", nil, Email("victim@example.com")); v.Allowed || v.Reason != ReasonRateLimited {
		t.Fatalf("third submission for one address from a new IP: got %+v, want %q", v, ReasonRateLimited)
	}
}

func TestInvalidEmailIsRejected(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), nil)
	for _, addr := range []string{"", "nodomain", "@nolocal", "missing@", "a@b@c"} {
		if v, _ := submit(t, g, c, "resume", "203.0.113.7:5555", nil, Email(addr)); v.Allowed || v.Reason != ReasonInvalidEmail {
			t.Errorf("address %q: got %+v, want %q", addr, v, ReasonInvalidEmail)
		}
	}
}

func TestDomainWithNoMailServerIsRejected(t *testing.T) {
	cases := map[string]*mxResolver{
		"no records": {records: nil},
		"nxdomain":   {err: &net.DNSError{Err: "no such host", Name: "bogus.invalid", IsNotFound: true}},
		"null MX":    {records: []*net.MX{{Host: ".", Pref: 0}}}, // RFC 7505: accepts no mail
	}
	for name, mx := range cases {
		g, c := testGuard(t, newFakeStore(), func(cfg *Config) { cfg.LookupMX = mx.LookupMX })
		if v, _ := submit(t, g, c, "resume", "203.0.113.7:5555", nil, Email("user@bogus.invalid")); v.Allowed || v.Reason != ReasonNoMX {
			t.Errorf("%s: got %+v, want %q", name, v, ReasonNoMX)
		}
	}
}

// A DNS hiccup is not evidence of a bot. Rejecting on it would turn away real
// people whenever the resolver has a bad minute, so a temporary failure passes.
func TestTemporaryMXLookupFailureAllows(t *testing.T) {
	mx := &mxResolver{err: &net.DNSError{Err: "i/o timeout", IsTimeout: true, IsTemporary: true}}
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) { cfg.LookupMX = mx.LookupMX })

	if v, err := submit(t, g, c, "resume", "203.0.113.7:5555", nil, Email("user@example.com")); err != nil || !v.Allowed {
		t.Fatalf("got %+v, %v; want allowed", v, err)
	}
}

// MX runs last: a rate-limited request must not make a DNS query, or the
// limiter would do nothing to bound the lookups a bot can make us perform.
func TestRateLimitedRequestDoesNoDNSLookup(t *testing.T) {
	mx := goodMX()
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) {
		cfg.IPLimit = Limit{Max: 1, Window: time.Hour}
		cfg.LookupMX = mx.LookupMX
	})

	_, _ = submit(t, g, c, "resume", "203.0.113.7:5555", nil, Email("a@example.com"))
	before := mx.Calls()
	if v, _ := submit(t, g, c, "resume", "203.0.113.7:5555", nil, Email("b@example.com")); v.Allowed {
		t.Fatal("second submission was allowed")
	}
	if mx.Calls() != before {
		t.Errorf("a rate-limited request made %d DNS lookup(s)", mx.Calls()-before)
	}
}

// Every consumer writes its submission to the same database the limiter uses.
// If the limiter cannot reach it, the form could not have saved anyway, and
// failing open would drop the limit exactly when something is already wrong.
func TestStoreErrorFailsClosed(t *testing.T) {
	store := newFakeStore()
	store.err = errStore
	g, c := testGuard(t, store, nil)

	v, err := submit(t, g, c, "contact", "203.0.113.7:5555", nil)
	if !errors.Is(err, errStore) {
		t.Fatalf("err = %v, want the store error wrapped", err)
	}
	if v.Allowed || v.Reason != ReasonUnavailable {
		t.Fatalf("got %+v, want rejected with %q", v, ReasonUnavailable)
	}
}

// The limiter needs equality, not identity: it must not store an address or an
// email in any recoverable form.
func TestLimiterStoresNoRawIdentifiers(t *testing.T) {
	store := newFakeStore()
	g, c := testGuard(t, store, nil)
	const ip, addr = "203.0.113.7", "victim@example.com"

	if _, err := submit(t, g, c, "resume", ip+":5555", nil, Email(addr)); err != nil {
		t.Fatal(err)
	}
	if len(store.params) != 2 {
		t.Fatalf("got %d counter writes, want 2 (ip and email)", len(store.params))
	}
	for _, p := range store.params {
		if len(p.KeyHash) != 32 {
			t.Errorf("%s key is %d bytes, want a 32-byte HMAC", p.KeyKind, len(p.KeyHash))
		}
		for _, raw := range []string{ip, addr} {
			if bytes.Contains(p.KeyHash, []byte(raw)) {
				t.Errorf("%s key contains %q", p.KeyKind, raw)
			}
		}
	}
}

// A rejection is a security event, logged once at info with what an operator
// needs to follow it up -- and never the token, which would let anyone reading
// the log replay it.
func TestRejectionIsLoggedAtInfo(t *testing.T) {
	logger, buf := logBuffer()
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) {
		cfg.Logger = logger
		cfg.RequestID = func(context.Context) string { return "req-123" }
	})
	tok := g.Token("contact")
	c.Advance(time.Second) // too fast
	r := request{remote: "203.0.113.7:5555", fields: url.Values{defaultTokenField: {tok}}}.build(t)
	if _, err := g.Check(context.Background(), r, "contact"); err != nil {
		t.Fatal(err)
	}

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("want exactly one JSON record, got %q: %v", buf.String(), err)
	}
	want := map[string]any{"level": "INFO", "msg": "antibot_rejected", "form": "contact",
		"reason": string(ReasonTooFast), "ip": "203.0.113.7", "request_id": "req-123"}
	for k, v := range want {
		if rec[k] != v {
			t.Errorf("%s = %v, want %v", k, rec[k], v)
		}
	}
	if bytes.Contains(buf.Bytes(), []byte(tok)) {
		t.Error("the rejection log contains the token")
	}
}

func TestAllowedRequestLogsNothing(t *testing.T) {
	logger, buf := logBuffer()
	g, c := testGuard(t, newFakeStore(), func(cfg *Config) { cfg.Logger = logger })

	if v, _ := submit(t, g, c, "contact", "203.0.113.7:5555", nil); !v.Allowed {
		t.Fatalf("rejected: %+v", v)
	}
	if buf.Len() != 0 {
		t.Errorf("an allowed request logged: %s", buf.String())
	}
}
