// Package handler is the HTTP layer of the faq module: route registration,
// request decoding, view-model assembly, response rendering, and error
// mapping.
//
// Handlers follow osg's two-shape pattern (DESIGN.md M3 decisions):
//
//   - Full pages: rendered through the base layout
//     (internal/render.Templates.Render with a "pages/..." name).
//   - HTMX fragments: returned when the request carries the HX-Request
//     header. The handler renders a "partials/..." template directly.
//
// Auth is opt-in: the read path tolerates a nil user. The Server expects
// callers to construct it with a populated [Deps] and to mount the
// returned [http.Handler] at the configured base path.
package handler
