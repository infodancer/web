package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/infodancer/web/timeline/db"
)

func (s *Server) handleEventRange(w http.ResponseWriter, r *http.Request) {
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

	q := r.URL.Query()
	direction := q.Get("direction")
	limitStr := q.Get("limit")
	categoryStr := q.Get("category")
	tagStr := q.Get("tag")

	limit := int32(50)
	if limitStr != "" {
		if n, err := strconv.ParseInt(limitStr, 10, 32); err == nil && n > 0 && n <= 200 {
			limit = int32(n)
		}
	}

	var categoryID int64
	if categoryStr != "" {
		if n, err := strconv.ParseInt(categoryStr, 10, 64); err == nil {
			categoryID = n
		}
	}

	var tagID int64
	if tagStr != "" {
		if n, err := strconv.ParseInt(tagStr, 10, 64); err == nil {
			tagID = n
		}
	}

	cursorDate := q.Get("cursor_date")
	cursorSortStr := q.Get("cursor_sort")
	cursorSort := int32(0)
	if cursorSortStr != "" {
		if n, err := strconv.ParseInt(cursorSortStr, 10, 32); err == nil {
			cursorSort = int32(n)
		}
	}

	ctx := r.Context()
	var events []eventCardData

	if direction == "backward" && cursorDate != "" {
		d, err := time.Parse("2006-01-02", cursorDate)
		if err != nil {
			http.Error(w, "invalid cursor_date", http.StatusBadRequest)
			return
		}
		pgd := pgtype.Date{Time: d, Valid: true}
		events, err = s.fetchEventsBefore(ctx, tl.ID, pgd, cursorSort, limit, categoryID, tagID)
		if err != nil {
			s.logger(r).Error("list events before", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	} else {
		d := time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC)
		if cursorDate != "" {
			parsed, err := time.Parse("2006-01-02", cursorDate)
			if err != nil {
				http.Error(w, "invalid cursor_date", http.StatusBadRequest)
				return
			}
			d = parsed
		}
		pgd := pgtype.Date{Time: d, Valid: true}
		events, err = s.fetchEventsAfter(ctx, tl.ID, pgd, cursorSort, limit, categoryID, tagID)
		if err != nil {
			s.logger(r).Error("list events after", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	s.renderPartial(w, "event_cards.gohtml", eventCardsData{
		BasePath:     s.basePath,
		TimelineSlug: slug,
		Events:       events,
	})
}

func (s *Server) handleEventDetail(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	eventSlug := r.PathValue("eventSlug")

	tl, err := s.store.GetTimeline(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.canViewTimeline(r.Context(), tl.IsPublic) {
		http.NotFound(w, r)
		return
	}

	event, err := s.store.GetEvent(r.Context(), db.GetEventParams{
		TimelineID: tl.ID,
		Slug:       eventSlug,
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Each related list below is best-effort: a failure logs and the detail
	// page still renders with whatever loaded.
	logger := s.logger(r)
	tags, err := s.store.ListEventTags(r.Context(), event.ID)
	if err != nil {
		logger.Warn("list event tags", "err", err)
	}

	people, err := s.store.ListEventPeople(r.Context(), event.ID)
	if err != nil {
		logger.Warn("list event people", "err", err)
	}

	linkedEvents, err := s.store.ListLinkedEvents(r.Context(), event.ID)
	if err != nil {
		logger.Warn("list linked events", "err", err)
	}

	sources, err := s.store.ListEventSources(r.Context(), event.ID)
	if err != nil {
		logger.Warn("list event sources", "err", err)
	}

	s.renderPage(w, r, "event_detail.gohtml", eventDetailData{
		BasePath:     s.basePath,
		Timeline:     tl,
		Event:        event,
		Tags:         tags,
		People:       people,
		LinkedEvents: linkedEvents,
		Sources:      sources,
	})
}

func (s *Server) handleEventInline(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	eventSlug := r.PathValue("eventSlug")

	tl, err := s.store.GetTimeline(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.canViewTimeline(r.Context(), tl.IsPublic) {
		http.NotFound(w, r)
		return
	}

	event, err := s.store.GetEvent(r.Context(), db.GetEventParams{
		TimelineID: tl.ID,
		Slug:       eventSlug,
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Each related list below is best-effort: a failure logs and the inline
	// fragment still renders with whatever loaded.
	logger := s.logger(r)
	tags, err := s.store.ListEventTags(r.Context(), event.ID)
	if err != nil {
		logger.Warn("list event tags", "err", err)
	}

	people, err := s.store.ListEventPeople(r.Context(), event.ID)
	if err != nil {
		logger.Warn("list event people", "err", err)
	}

	linkedEvents, err := s.store.ListLinkedEvents(r.Context(), event.ID)
	if err != nil {
		logger.Warn("list linked events", "err", err)
	}

	sources, err := s.store.ListEventSources(r.Context(), event.ID)
	if err != nil {
		logger.Warn("list event sources", "err", err)
	}

	s.renderPartial(w, "event_inline.gohtml", eventDetailData{
		BasePath:     s.basePath,
		Timeline:     tl,
		Event:        event,
		Tags:         tags,
		People:       people,
		LinkedEvents: linkedEvents,
		Sources:      sources,
	})
}

type eventCardData struct {
	ID            int64
	TimelineID    int64
	Slug          string
	Title         string
	Summary       string
	EventDate     time.Time
	DatePrecision string
	SortOrder     int32
	CategoryID    int64
	CategoryName  string
	CategoryColor string
}

type eventCardsData struct {
	BasePath     string
	TimelineSlug string
	Events       []eventCardData
}

type eventDetailData struct {
	BasePath     string
	Timeline     db.TimelineTimeline
	Event        db.GetEventRow
	Tags         []db.TimelineTag
	People       []db.ListEventPeopleRow
	LinkedEvents []db.ListLinkedEventsRow
	Sources      []db.TimelineEventSource
}

func toEventCard(id, timelineID int64, slug, title, summary string, eventDate pgtype.Date, precision string, sortOrder int32, categoryID pgtype.Int8, catName, catColor pgtype.Text) eventCardData {
	return eventCardData{
		ID:            id,
		TimelineID:    timelineID,
		Slug:          slug,
		Title:         title,
		Summary:       summary,
		EventDate:     eventDate.Time,
		DatePrecision: precision,
		SortOrder:     sortOrder,
		CategoryID:    categoryID.Int64,
		CategoryName:  catName.String,
		CategoryColor: catColor.String,
	}
}
