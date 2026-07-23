package handler

import (
	"errors"
	"net/http"

	"github.com/infodancer/web/profile/profileuser"
	"github.com/infodancer/web/profile/store"
)

// avatarFormMemory is the in-memory budget for parsing the multipart upload;
// larger files spill to temp storage. The actual per-image size cap is
// enforced by the media pipeline behind IngestAvatar (Config.Avatar.MaxUploadBytes).
const avatarFormMemory = 4 << 20

// avatarBodyLimit is a coarse ceiling on the whole multipart request, applied
// with http.MaxBytesReader before ParseMultipartForm so a client cannot
// exhaust memory or temp disk by streaming an unbounded body. It sits well
// above any legitimate avatar upload; the precise per-image cap lives
// downstream in the media pipeline.
const avatarBodyLimit = 12 << 20

// handleAvatarUpload ingests a new profile picture through the media pipeline
// (re-encode, metadata strip, content-address) and records its canonical URL.
// action=remove clears the stored URL, reverting to the identicon fallback.
// 404 when no ingest path is configured.
func (s *Server) handleAvatarUpload(w http.ResponseWriter, r *http.Request) {
	if s.ingestAvatar == nil {
		http.NotFound(w, r)
		return
	}
	u := profileuser.UserFromContext(r.Context())

	// Bound the request body before parsing so an unbounded upload can't
	// exhaust memory or temp disk; the media pipeline enforces the precise
	// per-image cap once the file is in hand.
	r.Body = http.MaxBytesReader(w, r.Body, avatarBodyLimit)
	if err := r.ParseMultipartForm(avatarFormMemory); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if r.PostFormValue("action") == "remove" {
		if err := s.setAvatarEnsuringRow(r, u, ""); err != nil {
			s.log.Error("avatar remove", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s.notifyChange(r.Context(), u.Key)
		http.Redirect(w, r, s.basePath+"/edit?saved=1", http.StatusSeeOther)
		return
	}

	file, _, err := r.FormFile("avatar")
	if err != nil {
		http.Error(w, "bad request: missing avatar file", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	url, err := s.ingestAvatar(r.Context(), u.Key, file)
	if err != nil {
		// The media pipeline rejects oversized, malformed, and unsupported
		// images; surface that as a client error, not a 500.
		s.log.Warn("avatar ingest rejected", "err", err)
		http.Error(w, "image rejected: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	if err := s.setAvatarEnsuringRow(r, u, url); err != nil {
		s.log.Error("avatar save", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.notifyChange(r.Context(), u.Key)
	http.Redirect(w, r, s.basePath+"/edit?saved=1", http.StatusSeeOther)
}

// setAvatarEnsuringRow records the avatar URL, creating the profile row first
// when the user uploads a picture before ever saving the form.
func (s *Server) setAvatarEnsuringRow(r *http.Request, u *profileuser.User, url string) error {
	err := s.store.SetAvatarURL(r.Context(), u.Key, url)
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	// IsPublic true matches the column default: an avatar-first user gets
	// the same listed-by-default profile a form-first user would.
	if _, uerr := s.store.Upsert(r.Context(), store.UpsertParams{
		UserKey:     u.Key,
		DisplayName: u.DisplayName,
		IsPublic:    true,
	}); uerr != nil {
		return uerr
	}
	return s.store.SetAvatarURL(r.Context(), u.Key, url)
}
