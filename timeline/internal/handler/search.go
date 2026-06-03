package handler

import (
	"log"
	"net/http"
	"strings"

	"github.com/infodancer/web/timeline/db"
)

type searchResultsData struct {
	TimelineSlug string
	Query        string
	Results      []db.SearchEventsRow
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
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

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		s.renderPartial(w, "search_results.html", searchResultsData{
			TimelineSlug: slug,
		})
		return
	}

	results, err := s.store.SearchEvents(r.Context(), db.SearchEventsParams{
		Query:      query,
		TimelineID: tl.ID,
		MaxResults: 50,
	})
	if err != nil {
		log.Printf("search events: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.renderPartial(w, "search_results.html", searchResultsData{
		TimelineSlug: slug,
		Query:        query,
		Results:      results,
	})
}
