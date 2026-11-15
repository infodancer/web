// Package httpsec is the infodancer/web suite's shared HTTP hardening: security
// response headers with a strict default Content-Security-Policy, a same-origin
// CSRF guard for state-changing requests, a request body limit, and
// http.Server defaults with graceful shutdown.
//
// It is host-side middleware, like [github.com/infodancer/web/obs]. A mounted
// component does not import it; the host wraps its mux:
//
//	h := httpsec.Chain(mux,
//		httpsec.Headers(httpsec.HeaderConfig{}),
//		httpsec.CSRF(httpsec.CSRFConfig{}),
//		httpsec.BodyLimit(1<<20),
//	)
//	srv := httpsec.NewServer(":8080", h, httpsec.ServerConfig{})
//	err := httpsec.Serve(ctx, srv, ln, 15*time.Second)
//
// Request IDs and panic recovery are in obs and are not repeated here.
//
// The defaults are deliberately strict. A site that needs a third-party origin
// (an analytics host, a font CDN) adds it to the [Policy] by name, so every
// relaxation is visible in the site's own code.
package httpsec
