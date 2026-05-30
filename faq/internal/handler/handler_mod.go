package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/faquser"
	"github.com/infodancer/web/faq/internal/slug"
	"github.com/infodancer/web/faq/store"
)

// M5 moderation handlers. All require an admin user; the requireAdmin
// helper writes 401 for anon and 403 for authed-but-non-admin so the
// host's login flow still kicks in when it should.
//
// Lock/unlock keep the row in place; the read path filters nothing
// based on locked status, but write gates (vote trigger, postAnswer,
// canEditAs with parentLocked) refuse mutations. Delete is a soft
// delete via the M5 DeleteQuestion query, which captures the prior
// status into prev_status so RestoreQuestion can put it back exactly
// where it was.
//
// After lock/unlock/restore the handler re-renders the question detail
// body so the htmx swap picks up the new status badge and the new set
// of moderation buttons. After delete the question disappears from
// every public view; htmx clients get an HX-Redirect to /q (host's
// base path + /q), plain form posts get 303 there.

// lockQuestion handles POST /q/{id}/lock. Idempotent on already-locked.
func (s *Server) lockQuestion(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	qid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := parseForm(r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	reason := strings.TrimSpace(postFormValue(r, "reason"))
	ctx := r.Context()
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}
	if _, err := s.deps.Store.GetQuestionIncludingDeleted(ctx, qid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get question", err)
		return
	}
	if err := s.deps.Store.LockQuestion(ctx, store.LockQuestionParams{
		ID: qid, ClosedByUserID: &local.ID, CloseReason: nullableString(reason),
	}); err != nil {
		s.internalError(w, r, "lock question", err)
		return
	}
	s.renderDetailAfterMod(w, r, qid, u, local.ID)
}

// unlockQuestion handles POST /q/{id}/unlock. Idempotent on already-open.
func (s *Server) unlockQuestion(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	qid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}
	if _, err := s.deps.Store.GetQuestionIncludingDeleted(ctx, qid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get question", err)
		return
	}
	if err := s.deps.Store.UnlockQuestion(ctx, qid); err != nil {
		s.internalError(w, r, "unlock question", err)
		return
	}
	s.renderDetailAfterMod(w, r, qid, u, local.ID)
}

// deleteQuestion handles POST /q/{id}/delete. Soft-deletes via the M5
// DeleteQuestion query (captures prev_status). Redirects to the
// question list because the row no longer renders.
func (s *Server) deleteQuestion(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	qid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := parseForm(r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	reason := strings.TrimSpace(postFormValue(r, "reason"))
	ctx := r.Context()
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}
	if _, err := s.deps.Store.GetQuestionIncludingDeleted(ctx, qid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get question", err)
		return
	}
	if err := s.deps.Store.DeleteQuestion(ctx, store.DeleteQuestionParams{
		ID: qid, ClosedByUserID: &local.ID, CloseReason: nullableString(reason),
	}); err != nil {
		s.internalError(w, r, "delete question", err)
		return
	}
	dest := s.deps.BasePath + "/q"
	if uiassets.IsRequest(r) {
		w.Header().Set("HX-Redirect", dest)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// restoreQuestion handles POST /q/{id}/restore. Sets status back to
// prev_status (COALESCE to 'open' for older rows without that bit).
// Redirects to the question's detail URL so the admin lands on it.
func (s *Server) restoreQuestion(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	qid, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	q, err := s.deps.Store.GetQuestionIncludingDeleted(ctx, qid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get question", err)
		return
	}
	if err := s.deps.Store.RestoreQuestion(ctx, qid); err != nil {
		s.internalError(w, r, "restore question", err)
		return
	}
	dest := fmt.Sprintf("%s/q/%d/%s", s.deps.BasePath, qid, slug.Normalize(q.Title))
	if uiassets.IsRequest(r) {
		w.Header().Set("HX-Redirect", dest)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// renderDetailAfterMod renders the question detail body so the htmx
// swap target picks up the new status. Falls back to a full-page
// redirect for non-htmx callers.
func (s *Server) renderDetailAfterMod(w http.ResponseWriter, r *http.Request, qid int64, u *faquser.User, localID int64) {
	if !uiassets.IsRequest(r) {
		// A non-htmx form post lands the admin back on the detail page.
		q, err := s.deps.Store.GetQuestion(r.Context(), qid)
		if err != nil {
			s.internalError(w, r, "get question after mod", err)
			return
		}
		http.Redirect(w, r,
			fmt.Sprintf("%s/q/%d/%s", s.deps.BasePath, qid, slug.Normalize(q.Title)),
			http.StatusSeeOther,
		)
		return
	}
	view, err := s.loadDetail(r.Context(), qid, u, localID)
	if err != nil {
		s.internalError(w, r, "load after mod", err)
		return
	}
	s.deps.Templates.Render(w, "partials/q_detail_body.gohtml", view)
}

// nullableString returns nil for an empty string so the *string pgx
// binding stores SQL NULL instead of an empty TEXT value.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
