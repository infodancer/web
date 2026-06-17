package handler

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/infodancer/web/timeline/db"
)

type indexData struct {
	Timelines []db.TimelineTimeline
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	timelines, err := s.store.ListPublicTimelines(r.Context())
	if err != nil {
		s.logger(r).Error("list timelines", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderPage(w, r, "index.gohtml", indexData{Timelines: timelines})
}

// laneAssignment maps a category to a side and column position.
type laneAssignment struct {
	CategoryID int64
	Name       string
	Color      string
	Side       string // "left" or "right"
	Index      int    // 0-based index within that side
}

type timelineData struct {
	Timeline   db.TimelineTimeline
	Events     []db.ListEventsAfterDateRow
	Categories []db.TimelineCategory
	Tags       []db.ListTagsRow
	Lanes      []laneAssignment
	LeftCount  int
	RightCount int
	EventCount int64
	MinDate    string
	MaxDate    string
}

// assignLanes distributes categories across left and right sides of the axis.
func assignLanes(categories []db.TimelineCategory) ([]laneAssignment, int, int) {
	total := len(categories) + 1 // +1 for uncategorized
	leftCount := (total + 1) / 2
	rightCount := total - leftCount

	lanes := make([]laneAssignment, 0, total)
	li, ri := 0, 0

	for i, cat := range categories {
		side := "left"
		idx := li
		if i%2 == 1 {
			side = "right"
			idx = ri
			ri++
		} else {
			li++
		}
		lanes = append(lanes, laneAssignment{
			CategoryID: cat.ID,
			Name:       cat.Name,
			Color:      cat.Color,
			Side:       side,
			Index:      idx,
		})
	}

	// Uncategorized
	side := "left"
	idx := li
	if len(categories)%2 == 1 {
		side = "right"
		idx = ri
	}
	lanes = append(lanes, laneAssignment{
		CategoryID: 0,
		Name:       "Uncategorized",
		Color:      "#6b7280",
		Side:       side,
		Index:      idx,
	})

	return lanes, leftCount, rightCount
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	logger := s.logger(r)
	slug := r.PathValue("slug")

	tl, err := s.store.GetTimeline(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.canViewTimeline(r.Context(), tl.IsPublic) {
		http.NotFound(w, r)
		return
	}

	categories, err := s.store.ListCategories(r.Context(), tl.ID)
	if err != nil {
		logger.Error("list categories", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	eventCount, err := s.store.CountEventsByTimeline(r.Context(), tl.ID)
	if err != nil {
		logger.Error("count events", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	bounds, err := s.store.GetTimelineDateBounds(r.Context(), tl.ID)
	if err != nil {
		// Degraded: missing bounds fall back to a default start date; the page
		// still renders.
		logger.Warn("get date bounds", "err", err)
	}

	var minDateStr, maxDateStr string
	startDate := pgtype.Date{Time: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}

	if bounds.MinDate.Valid {
		startDate = bounds.MinDate
		minDateStr = bounds.MinDate.Time.Format("2006-01-02")
	}
	if bounds.MaxDate.Valid {
		maxDateStr = bounds.MaxDate.Time.Format("2006-01-02")
	}

	events, err := s.store.ListEventsAfterDate(r.Context(), db.ListEventsAfterDateParams{
		TimelineID:     tl.ID,
		AfterDate:      pgtype.Date{Time: startDate.Time.AddDate(0, 0, -1), Valid: true},
		AfterSortOrder: -1,
		MaxResults:     50,
	})
	if err != nil {
		logger.Error("list initial events", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	tags, err := s.store.ListTags(r.Context(), tl.ID)
	if err != nil {
		// Degraded: the timeline renders without its tag filter list.
		logger.Warn("list tags", "err", err)
	}

	lanes, leftCount, rightCount := assignLanes(categories)

	s.renderPage(w, r, "timeline.gohtml", timelineData{
		Timeline:   tl,
		Events:     events,
		Categories: categories,
		Tags:       tags,
		Lanes:      lanes,
		LeftCount:  leftCount,
		RightCount: rightCount,
		EventCount: eventCount,
		MinDate:    minDateStr,
		MaxDate:    maxDateStr,
	})
}
