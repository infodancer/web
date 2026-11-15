// Package httpsec is the infodancer/web suite's shared HTTP hardening: security
// response headers with a strict default Content-Security-Policy, a same-origin
// CSRF guard for state-changing requests, a request body limit, and
// http.Server defaults with graceful shutdown.
//
// [Headers], [BodyLimit], [NewServer] and [Serve] are host-side, like
// [github.com/infodancer/web/obs]: the site that mounts the components wraps its
// mux with them. [CSRF] is also imported by components, because a component
// that serves its own state-changing form posts guards them itself (see
// CONVENTIONS.md, "Security"):
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
