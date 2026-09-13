package handler

import (
	"errors"
	"net"
	"strings"
	"testing"
)

// These cover the mail domain check the handler falls back to when no Guard is
// set. It lived in the newsletter/antibot package until that was removed.

func fakeMX(records []*net.MX, err error) func(string) ([]*net.MX, error) {
	return func(string) ([]*net.MX, error) { return records, err }
}

func TestValidateMX_ValidDomain(t *testing.T) {
	if err := validateMXWith("user@example.com", fakeMX([]*net.MX{{Host: "mx.example.com", Pref: 10}}, nil)); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateMX_NoRecords(t *testing.T) {
	err := validateMXWith("user@nodomain.invalid", fakeMX([]*net.MX{}, nil))
	if err == nil || !strings.Contains(err.Error(), "no MX records") {
		t.Errorf("got %v, want a 'no MX records' error", err)
	}
}

func TestValidateMX_DNSNotFound(t *testing.T) {
	err := validateMXWith("user@bogus.invalid", fakeMX(nil, &net.DNSError{Err: "no such host", Name: "bogus.invalid", IsNotFound: true}))
	if err == nil || !strings.Contains(err.Error(), "no MX records") {
		t.Errorf("got %v, want a 'no MX records' error", err)
	}
}

func TestValidateMX_LookupError(t *testing.T) {
	err := validateMXWith("user@example.com", fakeMX(nil, errors.New("network timeout")))
	if err == nil || !strings.Contains(err.Error(), "lookup failed") {
		t.Errorf("got %v, want a 'lookup failed' error", err)
	}
}

// A malformed address is rejected before any lookup is made.
func TestValidateMX_MalformedAddressMakesNoLookup(t *testing.T) {
	for _, addr := range []string{"not-an-email", "@example.com", "user@"} {
		called := false
		lookup := func(string) ([]*net.MX, error) { called = true; return nil, nil }
		if err := validateMXWith(addr, lookup); err == nil {
			t.Errorf("%q: expected an error", addr)
		}
		if called {
			t.Errorf("%q: made a DNS lookup for a malformed address", addr)
		}
	}
}
