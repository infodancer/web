package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/infodancer/web/timeline/db"
)

// ---- Shared JSON format ----

type timelineExport struct {
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description string          `json:"description,omitempty"`
	IsPublic    bool            `json:"is_public"`
	Categories  []categoryJSON  `json:"categories,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	Events      []eventJSON     `json:"events"`
	People      []personJSON    `json:"people,omitempty"`
	EventLinks  []eventLinkJSON `json:"event_links,omitempty"`
}

type categoryJSON struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Color     string `json:"color,omitempty"`
	SortOrder int    `json:"sort_order,omitempty"`
}

type eventJSON struct {
	Slug          string            `json:"slug"`
	Title         string            `json:"title"`
	Summary       string            `json:"summary,omitempty"`
	Article       string            `json:"article,omitempty"`
	Date          string            `json:"date"`
	EndDate       string            `json:"end_date,omitempty"`
	DatePrecision string            `json:"date_precision,omitempty"`
	SortOrder     int               `json:"sort_order,omitempty"`
	Category      string            `json:"category,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Sources       []sourceJSON      `json:"sources,omitempty"`
	People        []eventPersonJSON `json:"people,omitempty"`
}

type sourceJSON struct {
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Author   string `json:"author,omitempty"`
	Citation string `json:"citation,omitempty"`
}

type eventPersonJSON struct {
	Slug      string `json:"slug"`
	Role      string `json:"role,omitempty"`
	Explainer string `json:"explainer,omitempty"`
}

type personJSON struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	SortName string `json:"sort_name,omitempty"`
	Bio      string `json:"bio,omitempty"`
	BornDate string `json:"born_date,omitempty"`
	DiedDate string `json:"died_date,omitempty"`
	PhotoURL string `json:"photo_url,omitempty"`
}

type eventLinkJSON struct {
	EventA       string `json:"event_a"`
	EventB       string `json:"event_b"`
	Relationship string `json:"relationship,omitempty"`
	Description  string `json:"description,omitempty"`
}

// ---- Export ----

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	ctx := r.Context()

	tl, err := s.store.queries.GetTimeline(ctx, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	categories, err := s.store.queries.ListCategories(ctx, tl.ID)
	if err != nil {
		log.Printf("export categories: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	catMap := map[int64]string{}
	var catJSON []categoryJSON
	for _, c := range categories {
		catMap[c.ID] = c.Slug
		catJSON = append(catJSON, categoryJSON{
			Slug:      c.Slug,
			Name:      c.Name,
			Color:     c.Color,
			SortOrder: int(c.SortOrder),
		})
	}

	// Get all events
	allEvents, err := s.store.queries.ListEventsAfterDate(ctx, db.ListEventsAfterDateParams{
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
	eventSlugMap := map[int64]string{}
	var eventsJSON []eventJSON

	for _, e := range allEvents {
		eventSlugMap[e.ID] = e.Slug

		ej := eventJSON{
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

		// Tags
		eventTags, err := s.store.queries.ListEventTags(ctx, e.ID)
		if err == nil {
			for _, t := range eventTags {
				ej.Tags = append(ej.Tags, t.Slug)
				tagSet[t.Slug] = true
			}
		}

		// Sources
		sources, err := s.store.queries.ListEventSources(ctx, e.ID)
		if err == nil {
			for _, src := range sources {
				ej.Sources = append(ej.Sources, sourceJSON{
					URL:      src.Url,
					Title:    src.Title,
					Author:   src.Author,
					Citation: src.Citation,
				})
			}
		}

		// People
		people, err := s.store.queries.ListEventPeople(ctx, e.ID)
		if err == nil {
			for _, p := range people {
				ej.People = append(ej.People, eventPersonJSON{
					Slug:      p.Slug,
					Role:      p.Role,
					Explainer: p.Explainer,
				})
			}
		}

		eventsJSON = append(eventsJSON, ej)
	}

	// All tags
	var tags []string
	for t := range tagSet {
		tags = append(tags, t)
	}

	// People
	allPeople, err := s.store.queries.ListPeople(ctx, tl.ID)
	if err != nil {
		log.Printf("export people: %v", err)
	}
	var peopleJSON []personJSON
	for _, p := range allPeople {
		pj := personJSON{
			Slug:     p.Slug,
			Name:     p.Name,
			SortName: p.SortName,
			Bio:      p.Bio,
			PhotoURL: p.PhotoUrl,
		}
		if p.BornDate.Valid {
			pj.BornDate = p.BornDate.Time.Format("2006-01-02")
		}
		if p.DiedDate.Valid {
			pj.DiedDate = p.DiedDate.Time.Format("2006-01-02")
		}
		peopleJSON = append(peopleJSON, pj)
	}

	// Event links — need to query all linked events for each event
	var linksJSON []eventLinkJSON
	seen := map[string]bool{}
	for _, e := range allEvents {
		linked, err := s.store.queries.ListLinkedEvents(ctx, e.ID)
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
			linksJSON = append(linksJSON, eventLinkJSON{
				EventA:       a,
				EventB:       b,
				Relationship: l.Relationship,
				Description:  l.LinkDescription,
			})
		}
	}

	export := timelineExport{
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
	enc.Encode(export)
}

// ---- Import ----

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	ctx := r.Context()

	// Get or create timeline
	tl, err := s.store.queries.GetTimeline(ctx, slug)
	if err != nil {
		// Create new timeline from the import data
		var data timelineExport
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if data.Slug == "" {
			data.Slug = slug
		}
		if data.Name == "" {
			data.Name = slug
		}
		tl, err = s.store.queries.CreateTimeline(ctx, db.CreateTimelineParams{
			Slug:        data.Slug,
			Name:        data.Name,
			Description: data.Description,
			IsPublic:    data.IsPublic,
		})
		if err != nil {
			log.Printf("create timeline: %v", err)
			http.Error(w, "failed to create timeline: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.importData(w, ctx, tl, data)
		return
	}

	var data timelineExport
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.importData(w, ctx, tl, data)
}

func (s *Server) importData(w http.ResponseWriter, ctx context.Context, tl db.Timeline, data timelineExport) {
	// Categories: create, build slug->id map
	catMap := map[string]int64{}
	for _, c := range data.Categories {
		color := c.Color
		if color == "" {
			color = "#6366f1"
		}
		cat, err := s.store.queries.CreateCategory(ctx, db.CreateCategoryParams{
			TimelineID: tl.ID,
			Slug:       c.Slug,
			Name:       c.Name,
			Color:      color,
			SortOrder:  int32(c.SortOrder),
		})
		if err != nil {
			log.Printf("import category %s: %v", c.Slug, err)
			continue
		}
		catMap[c.Slug] = cat.ID
	}

	// Tags: get-or-create, build slug->id map
	tagMap := map[string]int64{}
	for _, tagSlug := range data.Tags {
		tag, err := s.store.queries.GetOrCreateTag(ctx, db.GetOrCreateTagParams{
			Slug: tagSlug,
			Name: strings.ReplaceAll(tagSlug, "-", " "),
		})
		if err != nil {
			log.Printf("import tag %s: %v", tagSlug, err)
			continue
		}
		tagMap[tagSlug] = tag.ID
	}
	// Also create tags referenced in events but not in top-level tags list
	for _, ev := range data.Events {
		for _, tagSlug := range ev.Tags {
			if _, ok := tagMap[tagSlug]; !ok {
				tag, err := s.store.queries.GetOrCreateTag(ctx, db.GetOrCreateTagParams{
					Slug: tagSlug,
					Name: strings.ReplaceAll(tagSlug, "-", " "),
				})
				if err == nil {
					tagMap[tagSlug] = tag.ID
				}
			}
		}
	}

	// People: create, build slug->id map
	personMap := map[string]int64{}
	for _, p := range data.People {
		params := db.CreatePersonParams{
			TimelineID: tl.ID,
			Slug:       p.Slug,
			Name:       p.Name,
			SortName:   p.SortName,
			Bio:        p.Bio,
			PhotoUrl:   p.PhotoURL,
		}
		if p.BornDate != "" {
			if t, err := time.Parse("2006-01-02", p.BornDate); err == nil {
				params.BornDate = pgtype.Date{Time: t, Valid: true}
			}
		}
		if p.DiedDate != "" {
			if t, err := time.Parse("2006-01-02", p.DiedDate); err == nil {
				params.DiedDate = pgtype.Date{Time: t, Valid: true}
			}
		}
		person, err := s.store.queries.CreatePerson(ctx, params)
		if err != nil {
			log.Printf("import person %s: %v", p.Slug, err)
			continue
		}
		personMap[p.Slug] = person.ID
	}

	// Events
	eventMap := map[string]int64{}
	imported := 0
	for _, ev := range data.Events {
		d, err := time.Parse("2006-01-02", ev.Date)
		if err != nil {
			log.Printf("import event %s: bad date %q", ev.Slug, ev.Date)
			continue
		}
		precision := ev.DatePrecision
		if precision == "" {
			precision = "day"
		}

		params := db.CreateEventParams{
			TimelineID:    tl.ID,
			Slug:          ev.Slug,
			Title:         ev.Title,
			Summary:       ev.Summary,
			Article:       ev.Article,
			EventDate:     pgtype.Date{Time: d, Valid: true},
			DatePrecision: precision,
			SortOrder:     int32(ev.SortOrder),
		}
		if ev.EndDate != "" {
			if t, err := time.Parse("2006-01-02", ev.EndDate); err == nil {
				params.EventEndDate = pgtype.Date{Time: t, Valid: true}
			}
		}
		if ev.Category != "" {
			if catID, ok := catMap[ev.Category]; ok {
				params.CategoryID = pgtype.Int8{Int64: catID, Valid: true}
			}
		}

		event, err := s.store.queries.CreateEvent(ctx, params)
		if err != nil {
			log.Printf("import event %s: %v", ev.Slug, err)
			continue
		}
		eventMap[ev.Slug] = event.ID
		imported++

		// Tags
		for _, tagSlug := range ev.Tags {
			if tagID, ok := tagMap[tagSlug]; ok {
				s.store.queries.AddEventTag(ctx, db.AddEventTagParams{
					EventID: event.ID,
					TagID:   tagID,
				})
			}
		}

		// Sources
		for i, src := range ev.Sources {
			s.store.queries.AddEventSource(ctx, db.AddEventSourceParams{
				EventID:   event.ID,
				Url:       src.URL,
				Title:     src.Title,
				Author:    src.Author,
				Citation:  src.Citation,
				SortOrder: int32(i),
			})
		}

		// People
		for i, ep := range ev.People {
			if personID, ok := personMap[ep.Slug]; ok {
				s.store.queries.AddEventPerson(ctx, db.AddEventPersonParams{
					EventID:   event.ID,
					PersonID:  personID,
					Role:      ep.Role,
					Explainer: ep.Explainer,
					SortOrder: int32(i),
				})
			}
		}
	}

	// Event links
	linksCreated := 0
	for _, link := range data.EventLinks {
		aID, aOk := eventMap[link.EventA]
		bID, bOk := eventMap[link.EventB]
		if !aOk || !bOk {
			continue
		}
		// Enforce a < b
		if aID > bID {
			aID, bID = bID, aID
		}
		err := s.store.queries.AddEventLink(ctx, db.AddEventLinkParams{
			EventAID:     aID,
			EventBID:     bID,
			Relationship: link.Relationship,
			Description:  link.Description,
		})
		if err == nil {
			linksCreated++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"timeline":   tl.Slug,
		"events":     imported,
		"categories": len(catMap),
		"tags":       len(tagMap),
		"people":     len(personMap),
		"links":      linksCreated,
	})
}
