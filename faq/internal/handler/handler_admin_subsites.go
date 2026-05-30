package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/infodancer/web/faq/store"
)

// adminSubsitesIndex handles GET /admin/subsites — list every subsite
// with its question count and links to edit / view.
func (s *Server) adminSubsitesIndex(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	ctx := r.Context()

	rows, err := s.deps.Store.ListSubsites(ctx)
	if err != nil {
		s.internalError(w, r, "admin subsites: list", err)
		return
	}
	out := make([]AdminSubsiteRow, len(rows))
	for i, sub := range rows {
		out[i] = AdminSubsiteRow{
			ID:            sub.ID,
			Slug:          sub.Slug,
			DisplayName:   sub.DisplayName,
			Description:   sub.Description,
			QuestionCount: int(sub.QuestionCount),
			EditURL:       fmt.Sprintf("%s/admin/subsites/%d", s.deps.BasePath, sub.ID),
			PublicURL:     fmt.Sprintf("%s/%s", s.deps.BasePath, sub.Slug),
		}
	}

	view := AdminSubsitesView{Subsites: out}
	base := s.baseView(r, view)
	base.PageTitle = "Subsites"
	s.deps.Templates.Render(w, "pages/admin_subsites.gohtml", base)
}

// adminSubsiteNew handles GET /admin/subsites/new — empty create form.
func (s *Server) adminSubsiteNew(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	view := AdminSubsiteFormView{
		ActionURL: s.deps.BasePath + "/admin/subsites",
		Heading:   "New subsite",
	}
	base := s.baseView(r, view)
	base.PageTitle = view.Heading
	s.deps.Templates.Render(w, "pages/admin_subsite_form.gohtml", base)
}

// adminSubsiteCreate handles POST /admin/subsites — validate, insert,
// redirect to the edit page so the admin can attach tag rules next.
func (s *Server) adminSubsiteCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	if err := parseForm(r); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	slugVal := strings.ToLower(strings.TrimSpace(postFormValue(r, "slug")))
	name := strings.TrimSpace(postFormValue(r, "display_name"))
	desc := strings.TrimSpace(postFormValue(r, "description"))

	verrs := s.validateSubsiteForm(slugVal, name)
	if len(verrs) > 0 {
		s.renderAdminSubsiteForm(w, r, 0, s.deps.BasePath+"/admin/subsites",
			"New subsite", slugVal, name, desc, nil, verrs, http.StatusUnprocessableEntity)
		return
	}

	created, err := s.deps.Store.CreateSubsite(r.Context(), store.CreateSubsiteParams{
		Slug: slugVal, DisplayName: name, Description: desc,
	})
	if err != nil {
		// Translate the unique-slug violation into a form error.
		verrs := []string{"slug already taken: " + slugVal}
		s.renderAdminSubsiteForm(w, r, 0, s.deps.BasePath+"/admin/subsites",
			"New subsite", slugVal, name, desc, nil, verrs, http.StatusConflict)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s/admin/subsites/%d", s.deps.BasePath, created.ID), http.StatusSeeOther)
}

// adminSubsiteEdit handles GET /admin/subsites/{id} — edit form with
// the attached tag rules.
func (s *Server) adminSubsiteEdit(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()

	sub, err := s.deps.Store.GetSubsiteByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "admin subsite: get", err)
		return
	}
	rules, err := s.deps.Store.ListTagRulesForSubsite(ctx, id)
	if err != nil {
		s.internalError(w, r, "admin subsite: tag rules", err)
		return
	}
	tagRows := make([]AdminSubsiteTagRow, len(rules))
	for i, r := range rules {
		tagRows[i] = AdminSubsiteTagRow{
			TagID:         r.ID,
			Name:          r.Name,
			QuestionCount: r.GlobalQuestionCount,
		}
	}
	pinned, err := s.loadExplicitSubsiteQuestions(ctx, id)
	if err != nil {
		s.internalError(w, r, "admin subsite: explicit questions", err)
		return
	}

	view := AdminSubsiteFormView{
		ID:                sub.ID,
		ActionURL:         fmt.Sprintf("%s/admin/subsites/%d", s.deps.BasePath, sub.ID),
		Heading:           "Edit subsite",
		Slug:              sub.Slug,
		DisplayName:       sub.DisplayName,
		Description:       sub.Description,
		TagRules:          tagRows,
		ExplicitQuestions: pinned,
	}
	base := s.baseView(r, view)
	base.PageTitle = "Edit " + sub.DisplayName
	s.deps.Templates.Render(w, "pages/admin_subsite_form.gohtml", base)
}

// adminSubsiteUpdate handles POST /admin/subsites/{id} — update slug,
// name, description.
func (s *Server) adminSubsiteUpdate(w http.ResponseWriter, r *http.Request) {
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
	slugVal := strings.ToLower(strings.TrimSpace(postFormValue(r, "slug")))
	name := strings.TrimSpace(postFormValue(r, "display_name"))
	desc := strings.TrimSpace(postFormValue(r, "description"))

	verrs := s.validateSubsiteForm(slugVal, name)
	if len(verrs) > 0 {
		// Re-fetch existing tag rules so the edit form re-renders with
		// the user's pending changes plus the unchanged attached tags.
		rules, _ := s.deps.Store.ListTagRulesForSubsite(r.Context(), id)
		tagRows := make([]AdminSubsiteTagRow, len(rules))
		for i, ru := range rules {
			tagRows[i] = AdminSubsiteTagRow{TagID: ru.ID, Name: ru.Name, QuestionCount: ru.GlobalQuestionCount}
		}
		s.renderAdminSubsiteForm(w, r, id,
			fmt.Sprintf("%s/admin/subsites/%d", s.deps.BasePath, id),
			"Edit subsite", slugVal, name, desc, tagRows, verrs, http.StatusUnprocessableEntity)
		return
	}

	if err := s.deps.Store.UpdateSubsite(r.Context(), store.UpdateSubsiteParams{
		ID: id, Slug: slugVal, DisplayName: name, Description: desc,
	}); err != nil {
		s.internalError(w, r, "admin subsite: update", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s/admin/subsites/%d", s.deps.BasePath, id), http.StatusSeeOther)
}

// adminSubsiteDelete handles POST /admin/subsites/{id}/delete.
// Cascades to faq_subsite_tags via the FK. faq_questions rows with
// subsite_id pointing at this subsite have their FK cleared by the
// default SET NULL — actually, the FK is plain REFERENCES with no
// action, which means PostgreSQL refuses the delete if any question
// still references it. The handler surfaces the error in that case;
// admins must re-scope the questions first.
func (s *Server) adminSubsiteDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.deps.Store.DeleteSubsite(r.Context(), id); err != nil {
		s.internalError(w, r, "admin subsite: delete", err)
		return
	}
	http.Redirect(w, r, s.deps.BasePath+"/admin/subsites", http.StatusSeeOther)
}

// adminSubsiteAttachTag handles POST /admin/subsites/{id}/tags. The
// form field "tag" carries the tag name to attach. GetOrCreateTag
// covers the case where the tag doesn't exist yet (rare since most
// tags arrive via imports, but useful when an admin pre-seeds a tag
// rule for a tag the next import will create).
func (s *Server) adminSubsiteAttachTag(w http.ResponseWriter, r *http.Request) {
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
	name := strings.ToLower(strings.TrimSpace(postFormValue(r, "tag")))
	if name == "" {
		http.Error(w, "tag is required", http.StatusBadRequest)
		return
	}
	if err := validateTagSlug(name); err != nil {
		// Reserved or malformed tag — reject before touching the DB.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tag, err := s.deps.Store.GetOrCreateTag(r.Context(), store.GetOrCreateTagParams{Name: name})
	if err != nil {
		s.internalError(w, r, "admin subsite: get tag", err)
		return
	}
	if err := s.deps.Store.AttachTagToSubsite(r.Context(), store.AttachTagToSubsiteParams{
		SubsiteID: id, TagID: tag.ID,
	}); err != nil {
		s.internalError(w, r, "admin subsite: attach tag", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s/admin/subsites/%d", s.deps.BasePath, id), http.StatusSeeOther)
}

// adminSubsiteDetachTag handles POST /admin/subsites/{id}/tags/{tag_id}/delete.
func (s *Server) adminSubsiteDetachTag(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	r = withUser(r, u)
	id, ok := s.pathID(w, r, "id")
	if !ok {
		return
	}
	tagIDRaw := r.PathValue("tag_id")
	tagID, err := strconv.ParseInt(tagIDRaw, 10, 64)
	if err != nil || tagID <= 0 {
		s.notFound(w, r)
		return
	}
	if err := s.deps.Store.DetachTagFromSubsite(r.Context(), store.DetachTagFromSubsiteParams{
		SubsiteID: id, TagID: tagID,
	}); err != nil {
		s.internalError(w, r, "admin subsite: detach tag", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s/admin/subsites/%d", s.deps.BasePath, id), http.StatusSeeOther)
}

// adminMoveQuestion handles POST /admin/q/{id}/move. Form field
// "subsite_id" carries the destination (or "0" / empty for ungrouping
// the question back to subsite_id NULL). The SQL guard
// (source_id IS NULL) means imported questions are silently no-op'd —
// the redirect still happens but the row is unchanged.
func (s *Server) adminMoveQuestion(w http.ResponseWriter, r *http.Request) {
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
	raw := strings.TrimSpace(postFormValue(r, "subsite_id"))
	var newSubsite *int64
	if raw != "" && raw != "0" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			http.Error(w, "bad subsite_id", http.StatusBadRequest)
			return
		}
		newSubsite = &n
	}

	if err := s.deps.Store.SetQuestionSubsite(r.Context(), store.SetQuestionSubsiteParams{
		ID: id, SubsiteID: newSubsite,
	}); err != nil {
		s.internalError(w, r, "admin move question", err)
		return
	}
	// Redirect back to the question — admins follow up by viewing it
	// in the new context.
	http.Redirect(w, r, fmt.Sprintf("%s/q/%d", s.deps.BasePath, id), http.StatusSeeOther)
}

// validateSubsiteForm checks the slug and name fields used by both
// create and update.
func (s *Server) validateSubsiteForm(slugVal, name string) []string {
	var verrs []string
	if slugVal == "" {
		verrs = append(verrs, "slug is required")
	} else if err := validateSubsiteSlug(slugVal); err != nil {
		switch {
		case errors.Is(err, errSlugReserved):
			verrs = append(verrs, "slug is reserved: "+slugVal)
		default:
			verrs = append(verrs, err.Error())
		}
	}
	if name == "" {
		verrs = append(verrs, "display name is required")
	}
	return verrs
}

// renderAdminSubsiteForm is the shared re-render-with-errors path for
// the create and edit forms.
func (s *Server) renderAdminSubsiteForm(w http.ResponseWriter, r *http.Request, id int64,
	action, heading, slugVal, name, desc string, tagRules []AdminSubsiteTagRow,
	errs []string, status int,
) {
	view := AdminSubsiteFormView{
		ID:          id,
		ActionURL:   action,
		Heading:     heading,
		Slug:        slugVal,
		DisplayName: name,
		Description: desc,
		TagRules:    tagRules,
		Errors:      errs,
	}
	base := s.baseView(r, view)
	base.PageTitle = heading
	w.WriteHeader(status)
	s.deps.Templates.Render(w, "pages/admin_subsite_form.gohtml", base)
}
