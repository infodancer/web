package handler

import (
	"fmt"
	"net/http"

	"github.com/infodancer/web/faq/store"
)

// directoryRecentLimit caps the recent-questions strip on the directory
// page. The directory is a browse surface, not a feed — the strip is
// here to seed clicks into questions readers haven't met yet, not to
// be exhaustive.
const directoryRecentLimit = 10

// directoryLanding renders the module's root `/` page. Replaces the
// M6a-3 directoryStub. Builds a card per subsite (display name,
// description, question count) plus a recent-questions strip across
// the entire corpus.
func (s *Server) directoryLanding(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.resolveUser(w, r); ok {
		r = withUser(r, u)
	} else {
		return
	}
	ctx := r.Context()

	subs, err := s.deps.Store.ListSubsites(ctx)
	if err != nil {
		s.internalError(w, r, "directory: list subsites", err)
		return
	}
	subsiteCards := make([]SubsiteSummary, len(subs))
	for i, sub := range subs {
		subsiteCards[i] = SubsiteSummary{
			Slug:          sub.Slug,
			DisplayName:   sub.DisplayName,
			Description:   sub.Description,
			QuestionCount: int(sub.QuestionCount),
		}
	}

	recent, err := s.deps.Store.ListQuestionsNewest(ctx, store.ListQuestionsNewestParams{
		LimitN:  directoryRecentLimit,
		OffsetN: 0,
	})
	if err != nil {
		s.internalError(w, r, "directory: list recent", err)
		return
	}
	recentRows := make([]DirectoryRecent, len(recent))
	for i, q := range recent {
		slug := slugFor(q.ID, q.Title)
		dr := DirectoryRecent{
			ID:       q.ID,
			Slug:     slug,
			Title:    q.Title,
			URL:      fmt.Sprintf("%s/q/%d/%s", s.deps.BasePath, q.ID, slug),
			Imported: q.SourceID != nil,
			Locked:   q.Status == store.FaqQuestionStatusLocked,
		}
		if q.CreatedAt.Valid {
			dr.CreatedAt = q.CreatedAt.Time
		}
		recentRows[i] = dr
	}

	view := DirectoryView{
		Subsites: subsiteCards,
		Recent:   recentRows,
	}
	base := s.baseView(r, view)
	base.PageTitle = "FAQ"
	s.deps.Templates.Render(w, "pages/directory.gohtml", base)
}
