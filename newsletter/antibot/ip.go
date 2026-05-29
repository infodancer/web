package antibot

import (
	"net"
	"net/http"
	"strings"
)

// clientIP extracts the client IP from X-Forwarded-For (first entry),
// falling back to RemoteAddr. This is appropriate when behind a trusted
// reverse proxy (e.g., Traefik).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
