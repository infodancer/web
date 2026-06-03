package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/infodancer/web/timeline/db"
	"github.com/infodancer/web/timeline/internal/transfer"
)

// handleExport streams a timeline as the JSON interchange document. Admin-only:
// the route is registered behind requireAdmin.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	ctx := r.Context()

	tl, err := s.store.GetTimeline(ctx, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	categories, err := s.store.ListCategories(ctx, tl.ID)
	if err != nil {
		log.Printf("export categories: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	catMap := map[int64]string{}
	var catJSON []transfer.Category
	for _, c := range categories {
		catMap[c.ID] = c.Slug
		catJSON = append(catJSON, transfer.Category{
			Slug:      c.Slug,
			Name:      c.Name,
			Color:     c.Color,
			SortOrder: int(c.SortOrder),
		})
	}

	allEvents, err := s.store.ListEventsAfterDate(ctx, db.ListEventsAfterDateParams{
		TimelineID:     tl.ID,
		AfterDate:      pgtype.Date{Time: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		AfterSortOrder: -1,
		MaxResults:     100000,
	})
	if err != nil {
		log.Printf("export events: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	tagSet := map[string]bool{}
	var eventsJSON []transfer.Event
	for _, e := range allEvents {
		ej := transfer.Event{
			Slug:          e.Slug,
			Title:         e.Title,
			Summary:       e.Summary,
			Article:       e.Article,
			Date:          e.EventDate.Time.Format("2006-01-02"),
			DatePrecision: e.DatePrecision,
			SortOrder:     int(e.SortOrder),
		}
		if e.EventEndDate.Valid {
			ej.EndDate = e.EventEndDate.Time.Format("2006-01-02")
		}
		if e.CategoryID.Valid {
			ej.Category = catMap[e.CategoryID.Int64]
		}
		if eventTags, err := s.store.ListEventTags(ctx, e.ID); err == nil {
			for _, t := range eventTags {
				ej.Tags = append(ej.Tags, t.Slug)
				tagSet[t.Slug] = true
			}
		}
		if sources, err := s.store.ListEventSources(ctx, e.ID); err == nil {
			for _, src := range sources {
				ej.Sources = append(ej.Sources, transfer.Source{
					URL: src.Url, Title: src.Title, Author: src.Author, Citation: src.Citation,
				})
			}
		}
		if people, err := s.store.ListEventPeople(ctx, e.ID); err == nil {
			for _, p := range people {
				ej.People = append(ej.People, transfer.EventPerson{
					Slug: p.Slug, Role: p.Role, Explainer: p.Explainer,
				})
			}
		}
		eventsJSON = append(eventsJSON, ej)
	}

	var tags []string
	for t := range tagSet {
		tags = append(tags, t)
	}

	allPeople, err := s.store.ListPeople(ctx, tl.ID)
	if err != nil {
		log.Printf("export people: %v", err)
	}
	var peopleJSON []transfer.Person
	for _, p := range allPeople {
		pj := transfer.Person{
			Slug: p.Slug, Name: p.Name, SortName: p.SortName, Bio: p.Bio, PhotoURL: p.PhotoUrl,
		}
		if p.BornDate.Valid {
			pj.BornDate = p.BornDate.Time.Format("2006-01-02")
		}
		if p.DiedDate.Valid {
			pj.DiedDate = p.DiedDate.Time.Format("2006-01-02")
		}
		peopleJSON = append(peopleJSON, pj)
	}

	var linksJSON []transfer.EventLink
	seen := map[string]bool{}
	for _, e := range allEvents {
		linked, err := s.store.ListLinkedEvents(ctx, e.ID)
		if err != nil {
			continue
		}
		for _, l := range linked {
			a, b := e.Slug, l.Slug
			if a > b {
				a, b = b, a
			}
			key := a + "|" + b
			if seen[key] {
				continue
			}
			seen[key] = true
			linksJSON = append(linksJSON, transfer.EventLink{
				EventA: a, EventB: b, Relationship: l.Relationship, Description: l.LinkDescription,
			})
		}
	}

	export := transfer.TimelineExport{
		Name:        tl.Name,
		Slug:        tl.Slug,
		Description: tl.Description,
		IsPublic:    tl.IsPublic,
		Categories:  catJSON,
		Tags:        tags,
		Events:      eventsJSON,
		People:      peopleJSON,
		EventLinks:  linksJSON,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, tl.Slug))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(export); err != nil {
		log.Printf("export encode: %v", err)
	}
}
