package antibot

import (
	"context"
	"errors"
	"net"
	"strings"
)

// normalizeEmail returns the address lowercased, so case cannot split a
// per-address budget, and its domain. ok is false unless there is exactly one
// "@" with something on both sides.
func normalizeEmail(raw string) (addr, domain string, ok bool) {
	addr = strings.ToLower(strings.TrimSpace(raw))
	at := strings.IndexByte(addr, '@')
	if at < 1 || at == len(addr)-1 || strings.Count(addr, "@") != 1 {
		return "", "", false
	}
	return addr, addr[at+1:], true
}

// mailDomain returns ReasonNoMX when domain provably accepts no mail: it does
// not exist, publishes no MX records, or publishes a null MX (RFC 7505).
//
// Any other lookup failure lets the request through. A resolver having a bad
// minute is not evidence of a bot, and rejecting on it would turn away real
// people. That failure is swallowed deliberately, so it is logged here, at warn.
func (g *Guard) mailDomain(ctx context.Context, form, domain string) Reason {
	records, err := g.lookupMX(ctx, domain)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return ReasonNoMX
		}
		g.log.WarnContext(ctx, "antibot_mx_lookup_failed", "form", form, "domain", domain, "err", err)
		return ""
	}
	if len(records) == 0 {
		return ReasonNoMX
	}
	if len(records) == 1 && (records[0].Host == "." || records[0].Host == "") {
		return ReasonNoMX
	}
	return ""
}
