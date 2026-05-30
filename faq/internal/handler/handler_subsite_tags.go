package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/store"
)

// subsiteTagsIndex renders /{subsite}/tags. Lists the tags actually
// used by questions reachable from this subsite — intersected, not the
// global tag list. The page reuses pages/tag_index.gohtml +
// partials/tag_list_body.gohtml; URLs on each tag link to the subsite-
// scoped short form `/{subsite}/{tag}` rather than the global form.
func (s *Server) subsiteTagsIndex(w http.ResponseWriter, r *http.Request) {
	sub, ok := subsiteFromContext(r.Context())
	if !ok {
		s.notFound(w, r)
		return
	}
	if u, ok := s.resolveUser(w, r); ok {
		r = withUser(r, u)
	} else {
		return
	}
	ctx := r.Context()
	subPrefix := s.deps.BasePath + "/" + sub.Slug

	// No pagination at v0.1 — same shape as the global /tags page. A
	// subsite-scoped index is bounded by the tags used in this subsite,
	// which is typically tens to a few hundred entries.
	const allTags = 10_000
	rows, err := s.deps.Store.ListTopTagsForSubsite(ctx, store.ListTopTagsForSubsiteParams{
		SubsiteID: &sub.ID,
		LimitN:    allTags,
	})
	if err != nil {
		s.internalError(w, r, "subsite tags index", err)
		return
	}
	tags := make([]TagSummary, len(rows))
	for i, t := range rows {
		ts := TagSummary{
			Name:          t.Name,
			URL:           subPrefix + "/" + t.Name, // short tag-filter form
			QuestionCount: int(t.QuestionCount),
		}
		if t.Description != nil {
			ts.Description = *t.Description
		}
		tags[i] = ts
	}

	view := TagIndexView{Tags: tags}
	base := s.baseView(r, view)
	base.PageTitle = sub.DisplayName + " — Tags"
	if uiassets.IsRequest(r) {
		s.deps.Templates.Render(w, "partials/tag_list_body.gohtml", view)
		return
	}
	s.deps.Templates.Render(w, "pages/tag_index.gohtml", base)
}

// subsiteTagDetail renders /{subsite}/tags/{name} OR the short form
// /{subsite}/{tag}. Both resolve to the same view: the intersection of
// "questions in this subsite" with "questions tagged with this name."
//
// For the short form, the dispatcher has already validated that the
// segment isn't reserved and isn't followed by another path component.
// For the long form, the name is the second path segment.
func (s *Server) subsiteTagDetail(w http.ResponseWriter, r *http.Request, tagName string) {
	sub, ok := subsiteFromContext(r.Context())
	if !ok {
		s.notFound(w, r)
		return
	}
	if u, ok := s.resolveUser(w, r); ok {
		r = withUser(r, u)
	} else {
		return
	}
	tagName = strings.ToLower(strings.TrimSpace(tagName))
	if tagName == "" {
		s.notFound(w, r)
		return
	}
	ctx := r.Context()
	subPrefix := s.deps.BasePath + "/" + sub.Slug

	// Confirm the tag exists in the global tag set first. A 404 here
	// covers typo'd tag names without forcing the more-expensive
	// subsite-intersected query.
	if _, err := s.deps.Store.GetTagByName(ctx, tagName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "subsite tag: lookup", err)
		return
	}

	limit, offset, page := s.pageBounds(r)
	qs, err := s.deps.Store.ListSubsiteQuestionsByTag(ctx, store.ListSubsiteQuestionsByTagParams{
		SubsiteID: &sub.ID,
		TagName:   tagName,
		LimitN:    limit,
		OffsetN:   offset,
	})
	if err != nil {
		s.internalError(w, r, "subsite tag: list", err)
		return
	}
	total, err := s.deps.Store.CountSubsiteQuestionsByTag(ctx, store.CountSubsiteQuestionsByTagParams{
		SubsiteID: &sub.ID,
		TagName:   tagName,
	})
	if err != nil {
		s.internalError(w, r, "subsite tag: count", err)
		return
	}

	// 404 if the tag exists globally but has no intersection with this
	// subsite — otherwise readers see a confusingly empty page for
	// e.g. `/wot/random-tag-from-elsewhere`.
	if total == 0 {
		s.notFound(w, r)
		return
	}

	rows := make([]questionRow, len(qs))
	for i, q := range qs {
		rows[i] = fromFaqQuestion(q)
	}
	cards, err := s.buildCardsForPrefix(ctx, rows, subPrefix)
	if err != nil {
		s.internalError(w, r, "subsite tag: cards", err)
		return
	}

	view := TagDetailView{
		Name:      tagName,
		Questions: cards,
		Page:      page,
		PageSize:  int(limit),
		Total:     int(total),
		HasMore:   int64(offset)+int64(len(cards)) < total,
	}
	base := s.baseView(r, view)
	base.PageTitle = sub.DisplayName + " — Tag: " + tagName
	if uiassets.IsRequest(r) {
		s.deps.Templates.Render(w, "partials/tag_questions_body.gohtml", view)
		return
	}
	s.deps.Templates.Render(w, "pages/tag_questions.gohtml", base)
}
