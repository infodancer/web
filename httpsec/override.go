package httpsec

import (
	"fmt"
	"maps"
	"net/http"
	"strings"
)

// overridable are the headers a route may replace. HSTS is a property of the
// whole host and is never overridden per route. The CSP is left out until it
// has its own per-route API.
var overridable = map[string]bool{
	"Permissions-Policy": true,
	"X-Frame-Options":    true,
	"Referrer-Policy":    true,
}

// HeaderOverride replaces chosen security headers on the routes it wraps. The
// rest keep the site's values from [Headers]. Build one with [Override].
type HeaderOverride struct {
	values map[string]string
}

// Override returns an override that sets each given header, name to exact
// value, in the same form [HeaderConfig.Expected] reports. A value replaces the
// site's value whole; nothing is merged, so a narrow exception is stated in full
// and reviewed as written.
//
// It panics, at startup, on a name that is not overridable or is not spelled in
// its canonical form (so a typo cannot silently do nothing), on an empty value,
// and on a value with a control character (header injection).
func Override(headers map[string]string) *HeaderOverride {
	if len(headers) == 0 {
		panic("httpsec: Override needs at least one header")
	}
	for name, value := range headers {
		if !overridable[name] {
			panic(fmt.Sprintf("httpsec: %q cannot be overridden per route", name))
		}
		if strings.TrimSpace(value) == "" {
			panic(fmt.Sprintf("httpsec: empty value for %s", name))
		}
		for _, r := range value {
			if r < 0x20 || r == 0x7f {
				panic(fmt.Sprintf("httpsec: control character in value for %s", name))
			}
		}
	}
	return &HeaderOverride{values: maps.Clone(headers)}
}

// Wrap applies the override to h. Put it inside [Headers] so it runs after the
// site values are set. As with Headers, h may still overwrite a header itself.
func (o *HeaderOverride) Wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := w.Header()
		for name, value := range o.values {
			hdr.Set(name, value)
		}
		h.ServeHTTP(w, r)
	})
}

// Expect returns a copy of the headers this override sets, for a checker that
// must expect exactly what the route sends.
func (o *HeaderOverride) Expect() map[string]string {
	return maps.Clone(o.values)
}
