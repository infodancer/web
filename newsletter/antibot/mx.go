package antibot

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// MXLookup is a function that returns MX records for a domain.
// The default implementation uses net.LookupMX.
type MXLookup func(domain string) ([]*net.MX, error)

// ValidateMX checks that the domain part of the email address has at least
// one MX record. Returns nil if valid, or an error describing the problem.
func ValidateMX(email string) error {
	return ValidateMXWith(email, net.LookupMX)
}

// ValidateMXWith is like ValidateMX but uses the provided lookup function,
// allowing tests to avoid real DNS queries.
func ValidateMXWith(email string, lookup MXLookup) error {
	at := strings.LastIndex(email, "@")
	if at < 1 || at >= len(email)-1 {
		return errors.New("invalid email format")
	}
	domain := email[at+1:]

	records, err := lookup(domain)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return fmt.Errorf("domain %q has no MX records", domain)
		}
		return fmt.Errorf("MX lookup failed for %q: %w", domain, err)
	}

	if len(records) == 0 {
		return fmt.Errorf("domain %q has no MX records", domain)
	}

	return nil
}
