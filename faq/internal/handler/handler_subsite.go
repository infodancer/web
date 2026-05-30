package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/infodancer/web/faq/store"
)

// subsiteCtxKey is the context key under which a resolved [store.FaqSubsites]
// is attached by [Server.subsiteDispatch]. Handlers reach it via
// [subsiteFromContext].
type subsiteCtxKey struct{}

// withSubsite returns a context carrying sub. The pointer is to make
// "no subsite in scope" representable as a typed nil without a wrapping
// sentinel; subsiteFromContext returns it as a value type.
func withSubsite(ctx context.Context, sub *store.FaqSubsites) context.Context {
	return context.WithValue(ctx, subsiteCtxKey{}, sub)
}

// subsiteFromContext returns the subsite previously attached by
// [Server.subsiteDispatch], or (zero, false) if none.
func subsiteFromContext(ctx context.Context) (store.FaqSubsites, bool) {
	v, ok := ctx.Value(subsiteCtxKey{}).(*store.FaqSubsites)
	if !ok || v == nil {
		return store.FaqSubsites{}, false
	}
	return *v, true
}

// subsiteDispatch is the middleware that decides whether a request
// belongs to a subsite or to the global (literal-route) namespace.
//
// Why a hand-rolled dispatcher instead of more ServeMux patterns: Go
// 1.22's ServeMux refuses to register patterns it can't unambiguously
// rank. `/q/{id}` and `/{subsite}/q` both match the literal path
// `/q/q` and neither is strictly more specific, so registering both
// panics at startup. Our reserved-slug list makes that ambiguity
// unreachable (the subsite "q" cannot exist), but ServeMux can't see
// the constraint. Routing subsite paths in front of the mux sidesteps
// the issue cleanly.
//
// Dispatch logic:
//
//  1. Empty path or first segment is a reserved slug or matches a
//     literal top-level route → fall through to the mux unchanged.
//  2. First segment looks like a subsite slug → look it up. On miss,
//     fall through (the mux's catch-all 404 handles it). On hit,
//     attach the subsite to the context and route the rest of the
//     path internally via [Server.routeSubsite].
//  3. Internal errors short-circuit with 500 directly.
func (s *Server) subsiteDispatch(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			next.ServeHTTP(w, r)
			return
		}
		first, rest, _ := strings.Cut(path, "/")

		if _, reserved := reservedSubsiteSlugs[first]; reserved {
			next.ServeHTTP(w, r)
			return
		}

		sub, err := s.deps.Store.GetSubsiteBySlug(r.Context(), first)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Not a known subsite — let the mux's catch-all 404
				// handle it. This keeps the 404 page consistent with
				// any other unknown URL.
				next.ServeHTTP(w, r)
				return
			}
			s.internalError(w, r, "subsite dispatch", err)
			return
		}

		r = r.WithContext(withSubsite(r.Context(), &sub))
		s.routeSubsite(w, r, rest)
	})
}

// routeSubsite dispatches a subsite-scoped request by the path segments
// remaining after the {subsite} slug. The subsite itself is already in
// the request context. Each branch maps to one of the M6a sub-milestones.
func (s *Server) routeSubsite(w http.ResponseWriter, r *http.Request, rest string) {
	if rest == "" {
		// Bare subsite landing — /{subsite}.
		s.subsiteLanding(w, r)
		return
	}
	seg1, seg2, _ := strings.Cut(rest, "/")

	switch seg1 {
	case "q":
		// /{subsite}/q (list) or /{subsite}/q/{id}[/{slug}] (detail).
		if seg2 == "" {
			s.subsiteList(w, r)
			return
		}
		s.subsiteDetail(w, r, rest)
		return
	case "tags":
		// /{subsite}/tags (index) or /{subsite}/tags/{name} (detail).
		if seg2 == "" {
			s.subsiteTagsIndex(w, r)
			return
		}
		// seg2 may carry further slashes for nested-tag URLs — there
		// aren't any in v0.1, so reject anything past one segment.
		if strings.Contains(seg2, "/") {
			s.notFound(w, r)
			return
		}
		s.subsiteTagDetail(w, r, seg2)
		return
	case "search":
		// /{subsite}/search — subsite-intersected search.
		s.subsiteSearch(w, r)
		return
	case "ask":
		// /{subsite}/ask — GET form and POST submit. Same handler
		// branches on r.Method.
		s.subsiteAsk(w, r)
		return
	}

	// Default: short tag-filter form /{subsite}/{tag}.
	if _, reserved := reservedTagSlugs[seg1]; reserved {
		// Unreachable in practice (each reserved word has its own case
		// above), but defensive: a future reserved word that lacks an
		// explicit case must not silently leak into the tag handler.
		s.notFound(w, r)
		return
	}
	// Disallow paths deeper than /{subsite}/{tag}: there is no
	// /{subsite}/{tag}/{anything} route.
	if seg2 != "" {
		s.notFound(w, r)
		return
	}
	s.subsiteTagDetail(w, r, seg1)
}
