package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/infodancer/web/faq/store"
)

// adminQuestionIncludeSubsite handles POST /admin/q/{id}/subsites with
// form field "subsite_id". Pins the question into the given subsite via
// faq_subsite_questions, then refreshes canonicals so any new
// unique-match collapses onto the right canonical URL. (M6c)
func (s *Server) adminQuestionIncludeSubsite(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	subsiteID, ok := s.parseSubsiteIDForm(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	if err := s.deps.Store.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
		SubsiteID: subsiteID, QuestionID: id,
	}); err != nil {
		s.internalError(w, r, "admin curate: include", err)
		return
	}
	if err := s.deps.Store.RecomputeCanonicalSubsites(ctx); err != nil {
		s.internalError(w, r, "admin curate: recompute canonicals", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s/q/%d", s.deps.BasePath, id), http.StatusSeeOther)
}

// adminQuestionRemoveSubsite handles POST /admin/q/{id}/subsites/{subsite_id}/delete.
// Removes an explicit pin (a no-op if the row never existed), then
// recomputes canonicals. (M6c)
func (s *Server) adminQuestionRemoveSubsite(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	subsiteID, ok := s.pathInt64(w, r, "subsite_id")
	if !ok {
		return
	}

	ctx := r.Context()
	if err := s.deps.Store.RemoveQuestionFromSubsite(ctx, store.RemoveQuestionFromSubsiteParams{
		SubsiteID: subsiteID, QuestionID: id,
	}); err != nil {
		s.internalError(w, r, "admin curate: remove", err)
		return
	}
	if err := s.deps.Store.RecomputeCanonicalSubsites(ctx); err != nil {
		s.internalError(w, r, "admin curate: recompute canonicals", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s/q/%d", s.deps.BasePath, id), http.StatusSeeOther)
}

// adminSubsiteIncludeBulk handles POST /admin/subsites/{id}/questions —
// the bulk include-by-IDs form on the subsite admin edit page. The
// "ids" field is whitespace- and comma-separated. Invalid or
// missing-question IDs are collected and surfaced; valid ones are
// pinned, and canonicals are recomputed once at the end. (M6c)
func (s *Server) adminSubsiteIncludeBulk(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := parseForm(r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	raw := postFormValue(r, "ids")

	ctx := r.Context()
	ids, badTokens := parseQuestionIDList(raw)
	missing := make([]string, 0)
	for _, qid := range ids {
		if _, err := s.deps.Store.GetQuestion(ctx, qid); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				missing = append(missing, strconv.FormatInt(qid, 10))
				continue
			}
			s.internalError(w, r, "admin curate: lookup question", err)
			return
		}
		if err := s.deps.Store.IncludeQuestionInSubsite(ctx, store.IncludeQuestionInSubsiteParams{
			SubsiteID: id, QuestionID: qid,
		}); err != nil {
			s.internalError(w, r, "admin curate: include", err)
			return
		}
	}
	if err := s.deps.Store.RecomputeCanonicalSubsites(ctx); err != nil {
		s.internalError(w, r, "admin curate: recompute canonicals", err)
		return
	}

	// On any rejections, re-render the edit page with errors highlighted.
	// On clean success, redirect back to the edit page.
	if len(badTokens) > 0 || len(missing) > 0 {
		s.renderAdminSubsiteEditWithCurateErrors(w, r, id, badTokens, missing)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s/admin/subsites/%d", s.deps.BasePath, id), http.StatusSeeOther)
}

// adminSubsiteRemoveQuestion handles POST
// /admin/subsites/{id}/questions/{question_id}/delete. Mirror of
// adminQuestionRemoveSubsite but addressed from the subsite admin page
// (so the redirect goes back there).
func (s *Server) adminSubsiteRemoveQuestion(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	qid, ok := s.pathInt64(w, r, "question_id")
	if !ok {
		return
	}

	ctx := r.Context()
	if err := s.deps.Store.RemoveQuestionFromSubsite(ctx, store.RemoveQuestionFromSubsiteParams{
		SubsiteID: id, QuestionID: qid,
	}); err != nil {
		s.internalError(w, r, "admin curate: remove", err)
		return
	}
	if err := s.deps.Store.RecomputeCanonicalSubsites(ctx); err != nil {
		s.internalError(w, r, "admin curate: recompute canonicals", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s/admin/subsites/%d", s.deps.BasePath, id), http.StatusSeeOther)
}

// parseSubsiteIDForm reads "subsite_id" from a POST body, validating
// non-zero. Writes a 400 on bad input and returns ok=false.
func (s *Server) parseSubsiteIDForm(w http.ResponseWriter, r *http.Request) (int64, bool) {
	if err := parseForm(r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return 0, false
	}
	raw := strings.TrimSpace(postFormValue(r, "subsite_id"))
	if raw == "" {
		http.Error(w, "subsite_id is required", http.StatusBadRequest)
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		http.Error(w, "bad subsite_id", http.StatusBadRequest)
		return 0, false
	}
	return n, true
}

// pathInt64 parses a non-zero positive int64 from r.PathValue(name);
// writes 404 on bad input. Centralizes the pattern of pathID for path
// segments other than "id".
func (s *Server) pathInt64(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	raw := r.PathValue(name)
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		s.notFound(w, r)
		return 0, false
	}
	return n, true
}

// parseQuestionIDList splits a whitespace- and comma-separated string
// of question IDs into a deduplicated, sorted slice plus a list of
// rejected tokens (non-numeric or non-positive). Empty input is fine —
// returns empty slices.
func parseQuestionIDList(raw string) (ids []int64, bad []string) {
	seen := make(map[int64]struct{})
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	for _, tok := range fields {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		n, err := strconv.ParseInt(tok, 10, 64)
		if err != nil || n <= 0 {
			bad = append(bad, tok)
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		ids = append(ids, n)
	}
	return ids, bad
}

// loadAdminCurateBlock returns the M6c curation block for a question:
// the set of subsites it is explicitly pinned into, and the set of
// subsites available to pin into. Always returns a non-nil pointer when
// IsAdmin so the template branches stay simple; the slices may be
// empty.
func (s *Server) loadAdminCurateBlock(ctx context.Context, questionID int64) (*AdminCurateBlock, error) {
	pinned, err := s.deps.Store.ListExplicitSubsitesForQuestion(ctx, questionID)
	if err != nil {
		return nil, fmt.Errorf("list explicit subsites: %w", err)
	}
	all, err := s.deps.Store.ListSubsites(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subsites: %w", err)
	}
	pinnedIDs := make(map[int64]struct{}, len(pinned))
	out := &AdminCurateBlock{
		Pinned:    make([]SubsiteLink, 0, len(pinned)),
		Available: make([]SubsiteLink, 0, len(all)),
	}
	for _, p := range pinned {
		pinnedIDs[p.ID] = struct{}{}
		out.Pinned = append(out.Pinned, SubsiteLink{
			ID:          p.ID,
			Slug:        p.Slug,
			DisplayName: p.DisplayName,
			URL:         fmt.Sprintf("%s/%s", s.deps.BasePath, p.Slug),
		})
	}
	for _, a := range all {
		if _, ok := pinnedIDs[a.ID]; ok {
			continue
		}
		out.Available = append(out.Available, SubsiteLink{
			ID:          a.ID,
			Slug:        a.Slug,
			DisplayName: a.DisplayName,
			URL:         fmt.Sprintf("%s/%s", s.deps.BasePath, a.Slug),
		})
	}
	return out, nil
}

// renderAdminSubsiteEditWithCurateErrors re-renders the admin subsite
// edit page with the bulk-include error chips set. Used when the
// /admin/subsites/{id}/questions form contains invalid or missing-row
// IDs — the valid IDs were still inserted, but the form surfaces what
// failed so the admin can correct and retry.
func (s *Server) renderAdminSubsiteEditWithCurateErrors(w http.ResponseWriter, r *http.Request, id int64, badTokens, missingIDs []string) {
	ctx := r.Context()

	sub, err := s.deps.Store.GetSubsiteByID(ctx, id)
	if err != nil {
		s.internalError(w, r, "admin subsite: get", err)
		return
	}
	rules, err := s.deps.Store.ListTagRulesForSubsite(ctx, id)
	if err != nil {
		s.internalError(w, r, "admin subsite: tag rules", err)
		return
	}
	tagRows := make([]AdminSubsiteTagRow, len(rules))
	for i, rr := range rules {
		tagRows[i] = AdminSubsiteTagRow{TagID: rr.ID, Name: rr.Name, QuestionCount: rr.GlobalQuestionCount}
	}
	pinned, err := s.loadExplicitSubsiteQuestions(ctx, id)
	if err != nil {
		s.internalError(w, r, "admin subsite: explicit questions", err)
		return
	}

	view := AdminSubsiteFormView{
		ID:                id,
		ActionURL:         fmt.Sprintf("%s/admin/subsites/%d", s.deps.BasePath, sub.ID),
		Heading:           "Edit subsite",
		Slug:              sub.Slug,
		DisplayName:       sub.DisplayName,
		Description:       sub.Description,
		TagRules:          tagRows,
		ExplicitQuestions: pinned,
		BulkBadTokens:     badTokens,
		BulkMissingIDs:    missingIDs,
	}
	base := s.baseView(r, view)
	base.PageTitle = "Edit " + sub.DisplayName
	w.WriteHeader(http.StatusUnprocessableEntity)
	s.deps.Templates.Render(w, "pages/admin_subsite_form.gohtml", base)
}

// loadExplicitSubsiteQuestions fetches the first page of explicit
// curations for a subsite, packaged as view-model rows. Used by the
// admin subsite edit page (M6c).
func (s *Server) loadExplicitSubsiteQuestions(ctx context.Context, subsiteID int64) ([]AdminSubsiteQuestionRow, error) {
	rows, err := s.deps.Store.ListExplicitSubsiteQuestions(ctx, store.ListExplicitSubsiteQuestionsParams{
		SubsiteID: subsiteID,
		LimitN:    int32(s.deps.PageSize), //nolint:gosec // server-configured value, validated at construction
		OffsetN:   0,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AdminSubsiteQuestionRow, len(rows))
	for i, q := range rows {
		out[i] = AdminSubsiteQuestionRow{
			ID:    q.ID,
			Title: q.Title,
			Slug:  slugFor(q.ID, q.Title),
			URL:   fmt.Sprintf("%s/q/%d/%s", s.deps.BasePath, q.ID, slugFor(q.ID, q.Title)),
		}
	}
	return out, nil
}
