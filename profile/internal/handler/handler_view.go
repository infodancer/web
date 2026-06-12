package handler

import (
	"errors"
	"net/http"

	"github.com/infodancer/web/profile/internal/identicon"
	"github.com/infodancer/web/profile/store"
)

// handleView renders the public profile page for a claimed handle.
func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	p, ok := s.profileForHandle(w, r)
	if !ok {
		return
	}
	s.renderPage(w, r, "profile.gohtml", http.StatusOK, profileView{
		Profile:   p,
		AvatarSrc: s.avatarSrc(p),
		Sections:  s.hostSections(r, p.UserKey, false),
	})
}

// handleIdenticon serves the deterministic SVG fallback avatar for a claimed
// handle. It exists for every profile with a handle, whether or not a picture
// was uploaded, so consumers can always fall back to it.
func (s *Server) handleIdenticon(w http.ResponseWriter, r *http.Request) {
	p, ok := s.profileForHandle(w, r)
	if !ok {
		return
	}
	// The SVG is built entirely from hash-derived integers (no seed content
	// is echoed), but serve it with belt-and-braces headers anyway: SVG can
	// script when navigated to directly, so forbid everything.
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(identicon.Generate(p.Handle))
}

// profileForHandle resolves the {handle} path value to a profile, writing a
// 404 and returning ok=false when the handle is malformed or unclaimed.
func (s *Server) profileForHandle(w http.ResponseWriter, r *http.Request) (*store.Profile, bool) {
	handle := normalizeHandle(r.PathValue("handle"))
	// Shape-check before touching the database; reserved names can never be
	// claimed, so they 404 like any other miss.
	if len(handle) < handleMinLen || len(handle) > handleMaxLen || !handleRe.MatchString(handle) {
		http.NotFound(w, r)
		return nil, false
	}
	p, err := s.store.GetByHandle(r.Context(), handle)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return nil, false
	}
	if err != nil {
		s.log.Error("profile by handle", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	return p, true
}
