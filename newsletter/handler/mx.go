package handler

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// validateMX checks that the domain of email has at least one MX record. The
// handler uses it only when no Guard is set; a Guard checks the domain itself.
func validateMX(email string) error {
	return validateMXWith(email, net.LookupMX)
}

// validateMXWith is validateMX over the given lookup, so tests need no DNS. A
// malformed address is rejected before any lookup is made.
func validateMXWith(email string, lookup func(domain string) ([]*net.MX, error)) error {
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
