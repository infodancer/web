// Package transfer defines timeline's JSON interchange format and the import
// routine that materializes it into the database. The format is shared by the
// admin export handler and by [Import], which the timeline module exposes as a
// library method so a host can load a timeline without an HTTP round trip.
package transfer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/infodancer/web/timeline/db"
	"github.com/infodancer/web/timeline/internal/slug"
)

// TimelineExport is the top-level interchange document for one timeline.
type TimelineExport struct {
	Name        string      `json:"name"`
	Slug        string      `json:"slug"`
	Description string      `json:"description,omitempty"`
	IsPublic    bool        `json:"is_public"`
	Categories  []Category  `json:"categories,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	Events      []Event     `json:"events"`
	People      []Person    `json:"people,omitempty"`
	EventLinks  []EventLink `json:"event_links,omitempty"`
}

// Category is a category lane in the interchange format.
type Category struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Color     string `json:"color,omitempty"`
	SortOrder int    `json:"sort_order,omitempty"`
}

// Event is a single event in the interchange format.
type Event struct {
	Slug          string        `json:"slug"`
	Title         string        `json:"title"`
	Summary       string        `json:"summary,omitempty"`
	Article       string        `json:"article,omitempty"`
	Date          string        `json:"date"`
	EndDate       string        `json:"end_date,omitempty"`
	DatePrecision string        `json:"date_precision,omitempty"`
	SortOrder     int           `json:"sort_order,omitempty"`
	Category      string        `json:"category,omitempty"`
	Tags          []string      `json:"tags,omitempty"`
	Sources       []Source      `json:"sources,omitempty"`
	People        []EventPerson `json:"people,omitempty"`
}

// Source is a citation attached to an event.
type Source struct {
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Author   string `json:"author,omitempty"`
	Citation string `json:"citation,omitempty"`
}

// EventPerson links a person to an event with a role.
type EventPerson struct {
	Slug      string `json:"slug"`
	Role      string `json:"role,omitempty"`
	Explainer string `json:"explainer,omitempty"`
}

// Person is a person in the interchange format.
type Person struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	SortName string `json:"sort_name,omitempty"`
	Bio      string `json:"bio,omitempty"`
	BornDate string `json:"born_date,omitempty"`
	DiedDate string `json:"died_date,omitempty"`
	PhotoURL string `json:"photo_url,omitempty"`
}

// EventLink is an undirected relationship between two events.
type EventLink struct {
	EventA       string `json:"event_a"`
	EventB       string `json:"event_b"`
	Relationship string `json:"relationship,omitempty"`
	Description  string `json:"description,omitempty"`
}

// Stats reports what an [Import] created.
type Stats struct {
	Categories int
	Tags       int
	People     int
	Events     int
	Links      int
}

// Import materializes data into the timeline identified by slug, creating the
// timeline if it does not exist. q must be transaction-bound by the caller (see
// store.WithTx) so a failed import rolls back cleanly. Unlike the legacy API
// handler, Import aborts on the first hard error rather than logging and
// continuing, so the caller gets an atomic all-or-nothing result.
func Import(ctx context.Context, q *db.Queries, slug string, data TimelineExport) (Stats, error) {
	var stats Stats

	tl, err := q.GetTimeline(ctx, slug)
	if err != nil {
		if data.Slug == "" {
			data.Slug = slug
		}
		if data.Name == "" {
			data.Name = slug
		}
		tl, err = q.CreateTimeline(ctx, db.CreateTimelineParams{
			Slug:        data.Slug,
			Name:        data.Name,
			Description: data.Description,
			IsPublic:    data.IsPublic,
		})
		if err != nil {
			return stats, fmt.Errorf("create timeline: %w", err)
		}
	}

	catMap := map[string]int64{}
	for _, c := range data.Categories {
		color := c.Color
		if color == "" {
			color = "#6366f1"
		}
		cat, err := q.CreateCategory(ctx, db.CreateCategoryParams{
			TimelineID: tl.ID,
			Slug:       c.Slug,
			Name:       c.Name,
			Color:      color,
			SortOrder:  int32(c.SortOrder),
		})
		if err != nil {
			return stats, fmt.Errorf("category %q: %w", c.Slug, err)
		}
		catMap[c.Slug] = cat.ID
		stats.Categories++
	}

	tagMap := map[string]int64{}
	ensureTag := func(tagSlug string) error {
		if _, ok := tagMap[tagSlug]; ok {
			return nil
		}
		tag, err := q.GetOrCreateTag(ctx, db.GetOrCreateTagParams{
			Slug: tagSlug,
			Name: strings.ReplaceAll(tagSlug, "-", " "),
		})
		if err != nil {
			return fmt.Errorf("tag %q: %w", tagSlug, err)
		}
		tagMap[tagSlug] = tag.ID
		stats.Tags++
		return nil
	}
	for _, tagSlug := range data.Tags {
		if err := ensureTag(tagSlug); err != nil {
			return stats, err
		}
	}

	personMap := map[string]int64{}
	for _, p := range data.People {
		params := db.CreatePersonParams{
			TimelineID: tl.ID,
			Slug:       p.Slug,
			Name:       p.Name,
			SortName:   p.SortName,
			Bio:        p.Bio,
			PhotoUrl:   p.PhotoURL,
			BornDate:   parseDate(p.BornDate),
			DiedDate:   parseDate(p.DiedDate),
		}
		person, err := q.CreatePerson(ctx, params)
		if err != nil {
			return stats, fmt.Errorf("person %q: %w", p.Slug, err)
		}
		personMap[p.Slug] = person.ID
		stats.People++
	}

	eventMap := map[string]int64{}
	for _, ev := range data.Events {
		d, err := time.Parse("2006-01-02", ev.Date)
		if err != nil {
			return stats, fmt.Errorf("event %q: bad date %q: %w", ev.Slug, ev.Date, err)
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
			EventEndDate:  parseDate(ev.EndDate),
			DatePrecision: precision,
			SortOrder:     int32(ev.SortOrder),
		}
		if ev.Category != "" {
			if catID, ok := catMap[ev.Category]; ok {
				params.CategoryID = pgtype.Int8{Int64: catID, Valid: true}
			}
		}
		event, err := q.CreateEvent(ctx, params)
		if err != nil {
			return stats, fmt.Errorf("event %q: %w", ev.Slug, err)
		}
		eventMap[ev.Slug] = event.ID
		stats.Events++

		for _, tagSlug := range ev.Tags {
			if err := ensureTag(tagSlug); err != nil {
				return stats, err
			}
			if err := q.AddEventTag(ctx, db.AddEventTagParams{EventID: event.ID, TagID: tagMap[tagSlug]}); err != nil {
				return stats, fmt.Errorf("event %q tag %q: %w", ev.Slug, tagSlug, err)
			}
		}
		for i, src := range ev.Sources {
			if _, err := q.AddEventSource(ctx, db.AddEventSourceParams{
				EventID:   event.ID,
				Url:       src.URL,
				Title:     src.Title,
				Author:    src.Author,
				Citation:  src.Citation,
				SortOrder: int32(i),
			}); err != nil {
				return stats, fmt.Errorf("event %q source: %w", ev.Slug, err)
			}
		}
		for i, ep := range ev.People {
			personID, ok := personMap[ep.Slug]
			if !ok {
				continue
			}
			if err := q.AddEventPerson(ctx, db.AddEventPersonParams{
				EventID:   event.ID,
				PersonID:  personID,
				Role:      ep.Role,
				Explainer: ep.Explainer,
				SortOrder: int32(i),
			}); err != nil {
				return stats, fmt.Errorf("event %q person %q: %w", ev.Slug, ep.Slug, err)
			}
		}
	}

	for _, link := range data.EventLinks {
		aID, aOk := eventMap[link.EventA]
		bID, bOk := eventMap[link.EventB]
		if !aOk || !bOk {
			continue
		}
		if aID > bID {
			aID, bID = bID, aID
		}
		if err := q.AddEventLink(ctx, db.AddEventLinkParams{
			EventAID:     aID,
			EventBID:     bID,
			Relationship: link.Relationship,
			Description:  link.Description,
		}); err != nil {
			return stats, fmt.Errorf("link %q-%q: %w", link.EventA, link.EventB, err)
		}
		stats.Links++
	}

	return stats, nil
}

// AutoSlug returns a URL-safe slug derived from title, for callers building an
// export programmatically.
func AutoSlug(title string) string { return slug.Make(title) }

func parseDate(s string) pgtype.Date {
	if s == "" {
		return pgtype.Date{}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}
