package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/internal/render"
	"github.com/infodancer/web/faq/internal/slug"
	"github.com/infodancer/web/faq/store"
)

// Length caps mirror common Q&A posting limits (see M4 prep
// thread). Enforced on every native create / edit; admin overrides do
// NOT bypass the size limits — keeping output sane is independent of
// who authored it.
const (
	maxTitleLen   = 150
	maxBodyLen    = 30_000
	maxCommentLen = 600
	maxTags       = 5
)

// askForm renders GET /q/new — the empty question form. Requires auth;
// anon callers get a 401 (host owns login).
func (s *Server) askForm(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	view := AskQuestionView{
		ActionURL: s.deps.BasePath + "/q",
		Heading:   "Ask a question",
	}
	base := s.baseView(r, view)
	base.PageTitle = "Ask a question"
	s.deps.Templates.Render(w, "pages/q_new.gohtml", base)
}

// askSubmit handles POST /q. Validates, normalizes tags, creates the
// question and its tag attachments inside a single transaction, then
// redirects to the new question's detail URL.
func (s *Server) askSubmit(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)

	if err := parseForm(r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(postFormValue(r, "title"))
	body := strings.TrimSpace(postFormValue(r, "body"))
	tagsRaw := postFormValue(r, "tags")

	verrs := validateQuestionInput(title, body, tagsRaw)
	tags := normalizeTags(tagsRaw)
	if len(tags) > maxTags {
		verrs = append(verrs, fmt.Sprintf("at most %d tags", maxTags))
	}
	if len(verrs) > 0 {
		s.renderAskError(w, r, s.deps.BasePath+"/q", "Ask a question", title, body, tagsRaw, verrs)
		return
	}

	ctx := r.Context()
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}

	bodyHTML := render.RenderMarkdownString(body)

	var qID int64
	err = s.deps.Store.WithTx(ctx, func(q *store.Queries) error {
		qr, err := q.CreateQuestion(ctx, store.CreateQuestionParams{
			Title: title, BodyMd: body, BodyHtml: bodyHTML, AuthorUserID: &local.ID,
		})
		if err != nil {
			return fmt.Errorf("create question: %w", err)
		}
		qID = qr.ID
		if err := attachTagsTx(ctx, q, qr.ID, tags); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		s.internalError(w, r, "create question", err)
		return
	}

	dest := fmt.Sprintf("%s/q/%d/%s", s.deps.BasePath, qID, slug.Normalize(title))
	if uiassets.IsRequest(r) {
		// htmx clients respect HX-Redirect for a soft redirect; fall
		// back to a 200-no-body if the client somehow sends HX-Request
		// for a POST that doesn't want a partial.
		w.Header().Set("HX-Redirect", dest)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// editQuestionForm renders GET /q/{id}/edit as an htmx-targetable
// fragment that replaces the question's <article> element. Validates
// the edit gate (author within grace, or admin) before exposing the
// form contents.
func (s *Server) editQuestionForm(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	q, err := s.deps.Store.GetQuestion(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "get question", err)
		return
	}
	local, err := s.resolveLocalUser(ctx, u)
	if err != nil {
		s.internalError(w, r, "resolve user", err)
		return
	}
	var createdAt = q.CreatedAt.Time
	if !s.canEditAs(u, local.ID, q.AuthorUserID, createdAt, q.Status == store.FaqQuestionStatusLocked) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	tags, err := s.deps.Store.ListTagsForQuestion(ctx, q.ID)
	if err != nil {
		s.internalError(w, r, "tags for question", err)
		return
	}
	tagNames := make([]string, len(tags))
	for i, t := range tags {
		tagNames[i] = t.Name
	}

	view := EditQuestionView{
		ID:    q.ID,
		Title: q.Title,
		Body:  q.BodyMd,
		Tags:  strings.Join(tagNames, ", "),
	}
	s.deps.Templates.Render(w, "partials/q_edit_form.gohtml", view)
}

// editQuestionSubmit handles PATCH /q/{id}. Same gate as the edit form
// (defense in depth — the gate runs on both surfaces). Re-renders the
// question's <article> fragment so htmx swaps it in place.
func (s *Server) editQuestionSubmit(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	if err := parseForm(r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(postFormValue(r, "title"))
	body := strings.TrimSpace(postFormValue(r, "body"))
	tagsRaw := postFormValue(r, "tags")
	tags := normalizeTags(tagsRaw)

	verrs := validateQuestionInput(title, body, tagsRaw)
	if len(tags) > maxTags {
		verrs = append(verrs, fmt.Sprintf("at most %d tags", maxTags))
	}
	if len(verrs) > 0 {
		// Re-render the edit form with the user's values + errors.
		s.deps.Templates.Render(w, "partials/q_edit_form.gohtml", EditQuestionView{
			ID: id, Title: title, Body: body, Tags: tagsRaw, Errors: verrs,
		})
		return
	}

	bodyHTML := render.RenderMarkdownString(body)

	err := s.deps.Store.WithTx(ctx, func(q *store.Queries) error {
		row, err := q.GetQuestion(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errQuestionNotFound
			}
			return fmt.Errorf("get: %w", err)
		}
		local, err := s.resolveLocalUser(ctx, u)
		if err != nil {
			return err
		}
		if !s.canEditAs(u, local.ID, row.AuthorUserID, row.CreatedAt.Time, row.Status == store.FaqQuestionStatusLocked) {
			return errEditForbidden
		}
		if err := q.EditQuestion(ctx, store.EditQuestionParams{
			ID: id, Title: title, BodyMd: body, BodyHtml: bodyHTML,
			LastEditorUserID: &local.ID,
		}); err != nil {
			return fmt.Errorf("edit: %w", err)
		}
		// Rewrite tag attachments: detach the old set, attach the new.
		// Keeps the implementation tiny vs. a diff-and-apply loop.
		existing, err := q.ListTagsForQuestion(ctx, id)
		if err != nil {
			return fmt.Errorf("list tags: %w", err)
		}
		for _, t := range existing {
			if err := q.DetachTagFromQuestion(ctx, store.DetachTagFromQuestionParams{
				QuestionID: id, TagID: t.ID,
			}); err != nil {
				return fmt.Errorf("detach %s: %w", t.Name, err)
			}
		}
		return attachTagsTx(ctx, q, id, tags)
	})
	if err != nil {
		if errors.Is(err, errEditForbidden) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if errors.Is(err, errQuestionNotFound) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "edit question", err)
		return
	}

	// Re-render the question article fragment so htmx swaps the form
	// out with the updated rendered question.
	view, err := s.loadDetail(ctx, id, u, 0)
	if err != nil {
		s.internalError(w, r, "load after edit", err)
		return
	}
	s.deps.Templates.Render(w, "partials/q_detail_body.gohtml", view)
}

// errEditForbidden is the loadDetail-style sentinel for an edit
// rejected by the gate.
var errEditForbidden = errors.New("handler: edit forbidden")

// validateQuestionInput returns a slice of human-readable validation
// errors, empty when input is acceptable.
func validateQuestionInput(title, body, _ string) []string {
	var verrs []string
	if title == "" {
		verrs = append(verrs, "title is required")
	} else if utf8Len(title) > maxTitleLen {
		verrs = append(verrs, fmt.Sprintf("title must be %d characters or fewer", maxTitleLen))
	}
	if body == "" {
		verrs = append(verrs, "body is required")
	} else if utf8Len(body) > maxBodyLen {
		verrs = append(verrs, fmt.Sprintf("body must be %d characters or fewer", maxBodyLen))
	}
	return verrs
}

// normalizeTags splits the raw tag input on commas, slug-normalizes
// each token, drops empties, and de-duplicates while preserving
// first-seen order. Output is the canonical list of tag names to
// attach.
//
// Commas only — never whitespace. Imported tags are slug-shaped (no internal
// spaces) and the form label says comma-separated; treating whitespace
// as a delimiter would silently slice multi-word entries the user
// might paste from elsewhere.
func normalizeTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	for _, f := range strings.Split(raw, ",") {
		n := slug.Normalize(f)
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

// attachTagsTx creates (if necessary) and attaches every tag in names
// to questionID. Tag names are assumed already normalized.
func attachTagsTx(ctx context.Context, q *store.Queries, questionID int64, names []string) error {
	for _, name := range names {
		tag, err := q.GetOrCreateTag(ctx, store.GetOrCreateTagParams{Name: name})
		if err != nil {
			return fmt.Errorf("create tag %q: %w", name, err)
		}
		if err := q.AttachTagToQuestion(ctx, store.AttachTagToQuestionParams{
			QuestionID: questionID, TagID: tag.ID,
		}); err != nil {
			return fmt.Errorf("attach tag %q: %w", name, err)
		}
	}
	return nil
}

// renderAskError re-renders the ask page with the user's input values
// and a list of validation errors. action is where the form POSTs to —
// global /q for the global ask, /{subsite}/ask for subsite-scoped.
func (s *Server) renderAskError(w http.ResponseWriter, r *http.Request, action, heading, title, body, tags string, errs []string) {
	view := AskQuestionView{
		ActionURL: action,
		Heading:   heading,
		Title:     title, Body: body, Tags: tags, Errors: errs,
	}
	base := s.baseView(r, view)
	base.PageTitle = heading
	w.WriteHeader(http.StatusUnprocessableEntity)
	s.deps.Templates.Render(w, "pages/q_new.gohtml", base)
}

// utf8Len returns the rune count of s, used for input length caps.
// The DB column types are TEXT and accept arbitrary length; the cap is
// a UX/spam control, not a storage constraint.
func utf8Len(s string) int { return len([]rune(s)) }
