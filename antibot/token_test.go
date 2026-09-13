package antibot

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// checkToken runs Check on a request carrying tok for form, with everything
// after the token check guaranteed to pass.
func checkToken(t *testing.T, g *Guard, form, tok string) Verdict {
	t.Helper()
	r := request{remote: "203.0.113.7:5555", fields: url.Values{defaultTokenField: {tok}}}.build(t)
	v, err := g.Check(context.Background(), r, form)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return v
}

func TestTokenAcceptedAfterMinFill(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), nil)
	tok := g.Token("contact")
	c.Advance(defaultMinFill + time.Second)

	if v := checkToken(t, g, "contact", tok); !v.Allowed {
		t.Fatalf("a token aged past the minimum fill time was rejected: %+v", v)
	}
}

// A human cannot fill a form in under the minimum; a script submitting the
// moment it fetched the page can.
func TestTokenRejectedWhenSubmittedTooFast(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), nil)
	tok := g.Token("contact")
	c.Advance(time.Second)

	if v := checkToken(t, g, "contact", tok); v.Allowed || v.Reason != ReasonTooFast {
		t.Fatalf("got %+v, want rejected with %q", v, ReasonTooFast)
	}
}

// A token scraped once must not work forever.
func TestTokenRejectedAfterMaxAge(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), nil)
	tok := g.Token("contact")
	c.Advance(defaultMaxAge + time.Second)

	if v := checkToken(t, g, "contact", tok); v.Allowed || v.Reason != ReasonTokenExpired {
		t.Fatalf("got %+v, want rejected with %q", v, ReasonTokenExpired)
	}
}

// The old newsletter token was an unsigned timestamp, so a bot could simply
// write one from an hour ago. Backdating a signed token breaks its signature.
func TestTokenRejectedWhenTimestampBackdated(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), nil)
	tok := g.Token("contact")
	_, mac, ok := strings.Cut(tok, ".")
	if !ok {
		t.Fatalf("token %q has no separator", tok)
	}
	forged := strconv.FormatInt(t0.Add(-time.Hour).Unix(), 10) + "." + mac
	c.Advance(time.Second) // too fast for the real token, fine for the forged one

	if v := checkToken(t, g, "contact", forged); v.Allowed || v.Reason != ReasonTokenInvalid {
		t.Fatalf("got %+v, want rejected with %q", v, ReasonTokenInvalid)
	}
}

// A token minted for the newsletter form is not a pass for the contact form.
func TestTokenRejectedForADifferentForm(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), nil)
	tok := g.Token("newsletter")
	c.Advance(defaultMinFill + time.Second)

	if v := checkToken(t, g, "contact", tok); v.Allowed || v.Reason != ReasonTokenInvalid {
		t.Fatalf("got %+v, want rejected with %q", v, ReasonTokenInvalid)
	}
}

func TestTokenRejectedUnderADifferentSecret(t *testing.T) {
	other, _ := testGuard(t, newFakeStore(), func(cfg *Config) {
		cfg.Secret = []byte("fedcba9876543210fedcba9876543210")
	})
	g, c := testGuard(t, newFakeStore(), nil)
	tok := other.Token("contact")
	c.Advance(defaultMinFill + time.Second)

	if v := checkToken(t, g, "contact", tok); v.Allowed || v.Reason != ReasonTokenInvalid {
		t.Fatalf("got %+v, want rejected with %q", v, ReasonTokenInvalid)
	}
}

// One server signs and checks, so a token from the future was not issued here.
func TestTokenRejectedWhenIssuedInTheFuture(t *testing.T) {
	ahead, aheadClock := testGuard(t, newFakeStore(), nil)
	aheadClock.Advance(time.Hour)
	g, _ := testGuard(t, newFakeStore(), nil)

	if v := checkToken(t, g, "contact", ahead.Token("contact")); v.Allowed || v.Reason != ReasonTokenInvalid {
		t.Fatalf("got %+v, want rejected with %q", v, ReasonTokenInvalid)
	}
}

func TestTokenMissingOrMalformed(t *testing.T) {
	g, c := testGuard(t, newFakeStore(), nil)
	c.Advance(time.Hour)

	cases := map[string]Reason{
		"":                ReasonTokenMissing,
		"garbage":         ReasonTokenInvalid,
		"1234567890":      ReasonTokenInvalid,
		"notanumber.AAAA": ReasonTokenInvalid,
		"1234567890.!!!!": ReasonTokenInvalid,
		".":               ReasonTokenInvalid,
	}
	for tok, want := range cases {
		if v := checkToken(t, g, "contact", tok); v.Allowed || v.Reason != want {
			t.Errorf("token %q: got %+v, want rejected with %q", tok, v, want)
		}
	}
}
