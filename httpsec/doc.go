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
// A policy can be rolled out safely: [CSPReportOnly] with a ReportURI blocks
// nothing and [ReportHandler] logs what it would have blocked.
//
// Request IDs and panic recovery are in obs and are not repeated here.
//
// The defaults are deliberately strict. A site that needs a third-party origin
// (an analytics host, a font CDN) adds it to the [Policy] by name, so every
// relaxation is visible in the site's own code.
//
// # The default policy is shared with smoke
//
// The default, narrow security policy is expressed by two things together: this
// package, which decides what a site sends, and github.com/infodancer/smoke,
// which decides what a probe expects. Neither controls the other, and they must
// stay in sync; the site template runs smoke against a server built on these
// defaults, so drift between them fails a test. Whether to add a restriction or
// loosen one is decided from first principles (what the browser feature does,
// who needs it, what abuse it allows), never because one side says so. A site is
// expected to conform to the defaults unless a specific exception is made for
// that site or route, with the reason written next to it. See [Override] and
// [HeaderConfig.Expected].
package httpsec
