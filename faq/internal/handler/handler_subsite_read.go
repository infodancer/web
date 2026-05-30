package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	uiassets "github.com/infodancer/ui"

	"github.com/infodancer/web/faq/faquser"
	"github.com/infodancer/web/faq/store"
)

// subsiteLandingRecentLimit caps the recent-questions strip on a
// subsite landing — same shape as the global directory page (see
// directoryRecentLimit), but scoped to a single subsite.
const subsiteLandingRecentLimit = 10

// subsiteTopTagsLimit caps the tag-chips strip on a subsite landing.
const subsiteTopTagsLimit = 12

// subsiteLanding renders /{subsite}: subsite description plus a recent-
// questions strip plus the top tags actually used by questions in this
// subsite. Per DESIGN.md §18.4 D8.5 the landing is the default subsite
// view and always renders this shape — empty description still gets
// the strip and tag chips, not a redirect.
func (s *Server) subsiteLanding(w http.ResponseWriter, r *http.Request) {
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

	rows, err := s.deps.Store.ListSubsiteQuestionsNewest(ctx, store.ListSubsiteQuestionsNewestParams{
		SubsiteID: &sub.ID,
		LimitN:    subsiteLandingRecentLimit,
		OffsetN:   0,
	})
	if err != nil {
		s.internalError(w, r, "subsite: recent questions", err)
		return
	}
	recent := make([]DirectoryRecent, len(rows))
	for i, q := range rows {
		slug := slugFor(q.ID, q.Title)
		dr := DirectoryRecent{
			ID:       q.ID,
			Slug:     slug,
			Title:    q.Title,
			URL:      fmt.Sprintf("%s/q/%d/%s", subPrefix, q.ID, slug),
			Imported: q.SourceID != nil,
			Locked:   q.Status == store.FaqQuestionStatusLocked,
		}
		if q.CreatedAt.Valid {
			dr.CreatedAt = q.CreatedAt.Time
		}
		recent[i] = dr
	}

	tags, err := s.deps.Store.ListTopTagsForSubsite(ctx, store.ListTopTagsForSubsiteParams{
		SubsiteID: &sub.ID,
		LimitN:    subsiteTopTagsLimit,
	})
	if err != nil {
		s.internalError(w, r, "subsite: top tags", err)
		return
	}
	tagLinks := make([]TagLink, len(tags))
	for i, t := range tags {
		tagLinks[i] = TagLink{Name: t.Name, URL: subPrefix + "/" + t.Name}
	}

	count, err := s.deps.Store.CountSubsiteQuestions(ctx, &sub.ID)
	if err != nil {
		s.internalError(w, r, "subsite: count", err)
		return
	}

	view := SubsiteLandingView{
		Slug:            sub.Slug,
		DisplayName:     sub.DisplayName,
		Description:     sub.Description,
		QuestionCount:   int(count),
		Recent:          recent,
		TopTags:         tagLinks,
		AskURL:          subPrefix + "/ask",
		AllQuestionsURL: subPrefix + "/q",
	}
	base := s.baseView(r, view)
	base.PageTitle = sub.DisplayName
	s.deps.Templates.Render(w, "pages/subsite_landing.gohtml", base)
}

// subsiteList renders /{subsite}/q — the subsite-scoped question list.
// Reuses the same sort= contract as the global list (newest, votes,
// unanswered) but scopes every query through the subsite.
func (s *Server) subsiteList(w http.ResponseWriter, r *http.Request) {
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

	sort := r.URL.Query().Get("sort")
	if sort == "" {
		sort = sortNewest
	}
	limit, offset, page := s.pageBounds(r)
	ctx := r.Context()
	subPrefix := s.deps.BasePath + "/" + sub.Slug

	rows, err := s.fetchSubsiteList(ctx, sub.ID, sort, limit, offset)
	if err != nil {
		if errors.Is(err, ErrUnknownSort) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "subsite list", err)
		return
	}

	total, err := s.deps.Store.CountSubsiteQuestions(ctx, &sub.ID)
	if err != nil {
		s.internalError(w, r, "subsite list: count", err)
		return
	}

	cards, err := s.buildCardsForPrefix(ctx, rows, subPrefix)
	if err != nil {
		s.internalError(w, r, "subsite list: build cards", err)
		return
	}

	view := SubsiteQuestionListView{
		Subsite: SubsiteSummary{
			Slug:        sub.Slug,
			DisplayName: sub.DisplayName,
			Description: sub.Description,
		},
		Title:     sub.DisplayName + " — " + listTitle(sort),
		Sort:      sort,
		BaseURL:   subPrefix,
		Questions: cards,
		Page:      page,
		PageSize:  int(limit),
		Total:     int(total),
		HasMore:   int64(offset)+int64(len(cards)) < total,
	}
	base := s.baseView(r, view)
	base.PageTitle = view.Title
	s.deps.Templates.Render(w, "pages/subsite_q_list.gohtml", base)
}

// fetchSubsiteList runs the right sort variant for a subsite-scoped
// list. Mirrors [Server.fetchList] but with subsite-aware queries.
func (s *Server) fetchSubsiteList(ctx context.Context, subID int64, sort string, limit, offset int32) ([]questionRow, error) {
	switch sort {
	case sortNewest, "":
		qs, err := s.deps.Store.ListSubsiteQuestionsNewest(ctx, store.ListSubsiteQuestionsNewestParams{
			SubsiteID: &subID, LimitN: limit, OffsetN: offset,
		})
		if err != nil {
			return nil, err
		}
		out := make([]questionRow, len(qs))
		for i, q := range qs {
			out[i] = fromFaqQuestion(q)
		}
		return out, nil

	case sortVotes:
		qs, err := s.deps.Store.ListSubsiteQuestionsByVotes(ctx, store.ListSubsiteQuestionsByVotesParams{
			SubsiteID: &subID, LimitN: limit, OffsetN: offset,
		})
		if err != nil {
			return nil, err
		}
		out := make([]questionRow, len(qs))
		for i, q := range qs {
			r := questionRow{
				ID: q.ID, Title: q.Title, BodyMD: q.BodyMd,
				Status: string(q.Status), SourceID: q.SourceID, SourceURL: q.SourceUrl,
			}
			if q.CreatedAt.Valid {
				r.CreatedAt = q.CreatedAt.Time
			}
			out[i] = r
		}
		return out, nil

	case sortUnanswered:
		qs, err := s.deps.Store.ListSubsiteQuestionsUnanswered(ctx, store.ListSubsiteQuestionsUnansweredParams{
			SubsiteID: &subID, LimitN: limit, OffsetN: offset,
		})
		if err != nil {
			return nil, err
		}
		out := make([]questionRow, len(qs))
		for i, q := range qs {
			out[i] = fromFaqQuestion(q)
		}
		return out, nil

	default:
		return nil, ErrUnknownSort
	}
}

// subsiteDetail renders /{subsite}/q/{id} (and /{id}/{slug}). 404s if
// the question doesn't belong to this subsite (either as a native row
// or via tag-rule match). The id and optional slug live in the path
// remainder rather than ServeMux path values because the subsite
// dispatcher hand-parses subsite routes.
//
// rest is the path remainder after the subsite slug, e.g. "q/123" or
// "q/123/some-slug".
func (s *Server) subsiteDetail(w http.ResponseWriter, r *http.Request, rest string) {
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

	// Parse "q/{id}" or "q/{id}/{slug}".
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] != "q" || parts[1] == "" {
		s.notFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		s.notFound(w, r)
		return
	}
	var urlSlug string
	if len(parts) >= 3 {
		urlSlug = parts[2]
	}
	if len(parts) > 3 {
		s.notFound(w, r)
		return
	}
	ctx := r.Context()
	subPrefix := s.deps.BasePath + "/" + sub.Slug

	// Resolve local user for write-affordance gates.
	curr := faquser.UserFromContext(ctx)
	var currLocalID int64
	if curr != nil {
		localRow, err := s.resolveLocalUser(ctx, curr)
		if err != nil {
			s.internalError(w, r, "subsite detail: resolve user", err)
			return
		}
		currLocalID = localRow.ID
	}

	in, err := s.deps.Store.IsQuestionInSubsite(ctx, store.IsQuestionInSubsiteParams{
		QuestionID: id,
		SubsiteID:  &sub.ID,
	})
	if err != nil {
		s.internalError(w, r, "subsite detail: membership", err)
		return
	}
	if !in {
		// Question exists but not in this subsite — same 404 as for an
		// unknown id, per DESIGN §18.4 D3.
		s.notFound(w, r)
		return
	}

	detail, err := s.loadDetail(ctx, id, curr, currLocalID)
	if err != nil {
		if errors.Is(err, errQuestionNotFound) {
			s.notFound(w, r)
			return
		}
		s.internalError(w, r, "subsite detail: load", err)
		return
	}

	// Canonical-slug redirect — same shape as global detail, but the
	// destination is the subsite-prefixed URL.
	if urlSlug != "" && urlSlug != detail.Question.Slug && !uiassets.IsRequest(r) {
		target := fmt.Sprintf("%s/q/%d/%s", subPrefix, detail.Question.ID, detail.Question.Slug)
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}

	// Canonical-URL behavior per D8.3 (revised): the canonical URL is
	// dictated by the question's canonical_subsite_id (computed by
	// store.RecomputeCanonicalSubsites). A subsite-scoped detail page may
	// itself be non-canonical when its subsite is one of several matches.
	canonicalURL := canonicalURLFor(s.deps.BasePath, detail)

	// Cross-subsite "see also" — every other subsite this question
	// belongs to.
	allMatching, err := s.deps.Store.ListSubsitesForQuestion(ctx, id)
	if err != nil {
		s.internalError(w, r, "subsite detail: see also", err)
		return
	}
	seeAlso := make([]SubsiteLink, 0, len(allMatching))
	for _, other := range allMatching {
		if other.ID == sub.ID {
			continue
		}
		seeAlso = append(seeAlso, SubsiteLink{
			Slug:        other.Slug,
			DisplayName: other.DisplayName,
			URL: fmt.Sprintf("%s/%s/q/%d/%s",
				s.deps.BasePath, other.Slug, detail.Question.ID, detail.Question.Slug),
		})
	}

	view := SubsiteDetailView{
		Subsite: SubsiteSummary{
			Slug:        sub.Slug,
			DisplayName: sub.DisplayName,
			Description: sub.Description,
		},
		Detail:       detail,
		SeeAlso:      seeAlso,
		CanonicalURL: canonicalURL,
	}
	base := s.baseView(r, view)
	base.PageTitle = detail.Question.Title + " — " + sub.DisplayName
	base.CanonicalURL = canonicalURL
	s.deps.Templates.Render(w, "pages/subsite_q_detail.gohtml", base)
}
