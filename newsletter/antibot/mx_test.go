package antibot

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

func fakeLookup(records []*net.MX, err error) MXLookup {
	return func(domain string) ([]*net.MX, error) {
		return records, err
	}
}

func TestValidateMX_ValidDomain(t *testing.T) {
	lookup := fakeLookup([]*net.MX{{Host: "mx.example.com", Pref: 10}}, nil)

	if err := ValidateMXWith("user@example.com", lookup); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateMX_NoRecords(t *testing.T) {
	lookup := fakeLookup([]*net.MX{}, nil)

	err := ValidateMXWith("user@nodomain.invalid", lookup)
	if err == nil {
		t.Fatal("expected error for domain with no MX records")
	}
	if !strings.Contains(err.Error(), "no MX records") {
		t.Errorf("expected 'no MX records' error, got %v", err)
	}
}

func TestValidateMX_DNSNotFound(t *testing.T) {
	lookup := fakeLookup(nil, &net.DNSError{
		Err:        "no such host",
		Name:       "bogus.invalid",
		IsNotFound: true,
	})

	err := ValidateMXWith("user@bogus.invalid", lookup)
	if err == nil {
		t.Fatal("expected error for DNS not found")
	}
	if !strings.Contains(err.Error(), "no MX records") {
		t.Errorf("expected 'no MX records' error, got %v", err)
	}
}

func TestValidateMX_LookupError(t *testing.T) {
	lookup := fakeLookup(nil, fmt.Errorf("network timeout"))

	err := ValidateMXWith("user@example.com", lookup)
	if err == nil {
		t.Fatal("expected error for lookup failure")
	}
	if !strings.Contains(err.Error(), "lookup failed") {
		t.Errorf("expected 'lookup failed' error, got %v", err)
	}
}

func TestValidateMX_InvalidEmail(t *testing.T) {
	lookup := fakeLookup(nil, nil) // Should never be called.

	cases := []string{"", "nodomain", "@nolocalpart", "missing@"}
	for _, email := range cases {
		if err := ValidateMXWith(email, lookup); err == nil {
			t.Errorf("expected error for %q", email)
		}
	}
}
