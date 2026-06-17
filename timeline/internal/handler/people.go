package handler

import (
	"net/http"

	"github.com/infodancer/web/timeline/db"
)

type personProfileData struct {
	Timeline db.TimelineTimeline
	Person   db.TimelinePerson
	Events   []db.ListPersonEventsRow
}

func (s *Server) handlePersonProfile(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	personSlug := r.PathValue("personSlug")

	tl, err := s.store.GetTimeline(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.canViewTimeline(r.Context(), tl.IsPublic) {
		http.NotFound(w, r)
		return
	}

	person, err := s.store.GetPerson(r.Context(), db.GetPersonParams{
		TimelineID: tl.ID,
		Slug:       personSlug,
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}

	events, err := s.store.ListPersonEvents(r.Context(), person.ID)
	if err != nil {
		// Degraded: the profile renders without the person's event list.
		s.logger(r).Warn("list person events", "err", err)
	}

	s.renderPage(w, r, "person_profile.gohtml", personProfileData{
		Timeline: tl,
		Person:   person,
		Events:   events,
	})
}

type peopleListData struct {
	Timeline db.TimelineTimeline
	People   []db.TimelinePerson
}

func (s *Server) handlePeopleList(w http.ResponseWriter, r *http.Request) {
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

	people, err := s.store.ListPeople(r.Context(), tl.ID)
	if err != nil {
		s.logger(r).Error("list people", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.renderPage(w, r, "people_list.gohtml", peopleListData{
		Timeline: tl,
		People:   people,
	})
}
