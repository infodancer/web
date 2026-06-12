package handler

import (
	"errors"
	"net/http"

	"github.com/infodancer/ui/mdedit"

	"github.com/infodancer/web/profile/profileuser"
	"github.com/infodancer/web/profile/store"
)

// editFormBodyLimit caps the urlencoded edit form. The largest field is the
// 2000-character bio; 64 KiB leaves generous headroom for links and encoding
// overhead while bounding hostile bodies.
const editFormBodyLimit = 64 << 10

// blankLinkRows is how many empty link inputs the form offers beyond the
// already-saved links (up to maxLinks total).
const blankLinkRows = 3

// handleEditForm renders the signed-in user's own profile form. A first-time
// visitor gets a fresh form seeded with the resolver's display name.
func (s *Server) handleEditForm(w http.ResponseWriter, r *http.Request) {
	u := profileuser.UserFromContext(r.Context())

	p, err := s.store.Get(r.Context(), u.Key)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// First visit: seed the form with the resolver's display name and
		// the listed-by-default visibility the column default carries.
		p = &store.Profile{UserKey: u.Key, DisplayName: u.DisplayName, IsPublic: true}
	case err != nil:
		s.log.Error("edit form: get profile", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.renderEdit(w, r, p, editView{
		Saved: r.URL.Query().Get("saved") == "1",
	})
}

// handleEditSave validates and persists the form, then redirects back with a
// flash. Validation failures re-render the form with the submitted values at
// 422 so nothing the user typed is lost.
func (s *Server) handleEditSave(w http.ResponseWriter, r *http.Request) {
	u := profileuser.UserFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, editFormBodyLimit)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var errs []string
	displayName, msg := validateDisplayName(r.PostFormValue("display_name"))
	if msg != "" {
		errs = append(errs, msg)
	}
	bio, msg := validateBio(r.PostFormValue("bio"))
	if msg != "" {
		errs = append(errs, msg)
	}
	links, linkErrs := parseLinks(r.PostForm["link_label"], r.PostForm["link_url"])
	errs = append(errs, linkErrs...)
	// Checkbox semantics: present means listed, absent means unlisted. The
	// form always renders the input, so an Upsert never accidentally
	// preserves a stale value.
	isPublic := r.PostFormValue("is_public") != ""

	// Handle changes are validated against the current row before any write.
	current, err := s.store.Get(r.Context(), u.Key)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.log.Error("edit save: get profile", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	currentHandle := ""
	if current != nil {
		currentHandle = current.Handle
	}
	handle := normalizeHandle(r.PostFormValue("handle"))
	wantsHandleChange := handle != "" && handle != currentHandle
	if wantsHandleChange {
		if msg := validateHandle(handle, s.reserved); msg != "" {
			errs = append(errs, msg)
		} else if currentHandle != "" && !u.Admin {
			errs = append(errs, "handle cannot be changed once claimed")
		}
	}

	submitted := &store.Profile{
		UserKey:     u.Key,
		Handle:      currentHandle,
		DisplayName: displayName,
		BioMD:       bio,
		Links:       links,
		IsPublic:    isPublic,
	}
	if current != nil {
		submitted.AvatarURL = current.AvatarURL
	}
	if wantsHandleChange {
		// Echo the attempted handle back into the form on error.
		submitted.Handle = handle
	}
	if len(errs) > 0 {
		s.renderEditStatus(w, r, submitted, editView{Errors: errs}, http.StatusUnprocessableEntity)
		return
	}

	if _, err := s.store.Upsert(r.Context(), store.UpsertParams{
		UserKey:     u.Key,
		DisplayName: displayName,
		BioMD:       bio,
		Links:       links,
		IsPublic:    isPublic,
	}); err != nil {
		s.log.Error("edit save: upsert", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if wantsHandleChange {
		var herr error
		if currentHandle == "" {
			herr = s.store.ClaimHandle(r.Context(), u.Key, handle)
		} else {
			herr = s.store.SetHandle(r.Context(), u.Key, handle)
		}
		switch {
		case errors.Is(herr, store.ErrHandleTaken):
			s.renderEditStatus(w, r, submitted, editView{Errors: []string{"that handle is already taken"}}, http.StatusUnprocessableEntity)
			return
		case errors.Is(herr, store.ErrHandleAlreadySet):
			s.renderEditStatus(w, r, submitted, editView{Errors: []string{"handle cannot be changed once claimed"}}, http.StatusUnprocessableEntity)
			return
		case herr != nil:
			s.log.Error("edit save: handle", "err", herr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	s.notifyChange(r.Context(), u.Key)
	http.Redirect(w, r, s.basePath+"/edit?saved=1", http.StatusSeeOther)
}

// renderEdit renders the edit page at 200.
func (s *Server) renderEdit(w http.ResponseWriter, r *http.Request, p *store.Profile, v editView) {
	s.renderEditStatus(w, r, p, v, http.StatusOK)
}

// renderEditStatus fills the shared editView fields from p and renders the
// edit page at the given status.
func (s *Server) renderEditStatus(w http.ResponseWriter, r *http.Request, p *store.Profile, v editView, status int) {
	u := profileuser.UserFromContext(r.Context())
	v.Profile = p
	v.HandleLocked = p.Handle != "" && !u.Admin
	v.AvatarUploads = s.ingestAvatar != nil
	v.AvatarSrc = s.avatarSrc(p)
	v.LinkRows = padLinkRows(p.Links)
	v.Sections = s.hostSections(r, u.Key, true)
	// The bio rides in profile's own form via mdedit/field: no preview or
	// upload endpoints, a comment-grade toolbar matching the markdown.Comment
	// preset the bio renders through.
	v.BioField = mdedit.Field{
		ID:        "profile-bio",
		Name:      "bio",
		Markdown:  p.BioMD,
		Label:     "About you",
		Rows:      5,
		MaxLength: bioMaxLen,
		Toolbar:   "standard",
	}
	s.renderPageWith(w, r, "edit.gohtml", status, v, func(b *BaseView) {
		b.EditorHead = mdedit.HeadTags(s.basePath + "/static/mdedit")
	})
}

// padLinkRows appends blank rows after the saved links so the form always
// offers room to add more, capped at maxLinks total.
func padLinkRows(links []store.Link) []store.Link {
	rows := make([]store.Link, len(links), len(links)+blankLinkRows)
	copy(rows, links)
	for len(rows) < len(links)+blankLinkRows && len(rows) < maxLinks {
		rows = append(rows, store.Link{})
	}
	return rows
}
