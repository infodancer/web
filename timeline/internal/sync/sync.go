// Package sync reads a CUE directory tree and upserts timeline data into the database.
//
// Directory structure:
//
//	mydata/
//	  timeline.cue          # timeline metadata
//	  categories/*.cue      # one file per category
//	  people/*.cue          # one file per person
//	  events/*.cue          # one file per event
//
// Schema-owned fields are updated on conflict. DB-only content
// (e.g. comments added via the web UI) is never touched.
package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/infodancer/web/timeline/db"
	"github.com/infodancer/web/timeline/internal/images"
)

// Runner holds the database queries and runs the sync.
type Runner struct {
	queries  *db.Queries
	ctx      context.Context
	dir      string
	imageDir string
	stats    Stats
}

type Stats struct {
	Categories int
	People     int
	Events     int
	Tags       int
	Links      int
	Sources    int
	Images     int
}

func New(ctx context.Context, queries *db.Queries, dir, imageDir string) *Runner {
	return &Runner{queries: queries, ctx: ctx, dir: dir, imageDir: imageDir}
}

func (r *Runner) Run() (Stats, error) {
	cctx := cuecontext.New()

	// 1. Timeline metadata
	tl, err := r.syncTimeline(cctx)
	if err != nil {
		return r.stats, fmt.Errorf("sync timeline: %w", err)
	}
	log.Printf("sync: timeline %q (id=%d)", tl.Slug, tl.ID)

	// 2. Categories
	catMap, err := r.syncCategories(cctx, tl.ID)
	if err != nil {
		return r.stats, fmt.Errorf("sync categories: %w", err)
	}

	// 3. People
	personMap, err := r.syncPeople(cctx, tl.ID, tl.Slug)
	if err != nil {
		return r.stats, fmt.Errorf("sync people: %w", err)
	}

	// 4. Events (with inline tags, sources, people links)
	err = r.syncEvents(cctx, tl.ID, catMap, personMap)
	if err != nil {
		return r.stats, fmt.Errorf("sync events: %w", err)
	}

	return r.stats, nil
}

// ---- Timeline ----

type timelineCUE struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	IsPublic    *bool  `json:"is_public"`
}

func (r *Runner) syncTimeline(cctx *cue.Context) (db.Timeline, error) {
	data, err := readCUEFile(cctx, filepath.Join(r.dir, "timeline.cue"))
	if err != nil {
		return db.Timeline{}, err
	}
	var t timelineCUE
	if err := json.Unmarshal(data, &t); err != nil {
		return db.Timeline{}, fmt.Errorf("decode timeline.cue: %w", err)
	}
	isPublic := true
	if t.IsPublic != nil {
		isPublic = *t.IsPublic
	}
	return r.queries.UpsertTimeline(r.ctx, db.UpsertTimelineParams{
		Slug:        t.Slug,
		Name:        t.Name,
		Description: t.Description,
		IsPublic:    isPublic,
	})
}

// ---- Categories ----

type categoryCUE struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	SortOrder int32  `json:"sort_order"`
}

func (r *Runner) syncCategories(cctx *cue.Context, tlID int64) (map[string]int64, error) {
	catMap := map[string]int64{}
	files, _ := filepath.Glob(filepath.Join(r.dir, "categories", "*.cue"))
	for _, f := range files {
		data, err := readCUEFile(cctx, f)
		if err != nil {
			log.Printf("sync: skip %s: %v", f, err)
			continue
		}
		var c categoryCUE
		if err := json.Unmarshal(data, &c); err != nil {
			log.Printf("sync: skip %s: %v", f, err)
			continue
		}
		if c.Color == "" {
			c.Color = "#6366f1"
		}
		cat, err := r.queries.UpsertCategory(r.ctx, db.UpsertCategoryParams{
			TimelineID: tlID,
			Slug:       c.Slug,
			Name:       c.Name,
			Color:      c.Color,
			SortOrder:  c.SortOrder,
		})
		if err != nil {
			log.Printf("sync: category %s: %v", c.Slug, err)
			continue
		}
		catMap[c.Slug] = cat.ID
		r.stats.Categories++
	}
	return catMap, nil
}

// ---- People ----

type personCUE struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	SortName string `json:"sort_name"`
	Bio      string `json:"bio"`
	BornDate string `json:"born_date"`
	DiedDate string `json:"died_date"`
	PhotoURL string `json:"photo_url"`
}

func (r *Runner) syncPeople(cctx *cue.Context, tlID int64, tlSlug string) (map[string]int64, error) {
	personMap := map[string]int64{}
	files, _ := filepath.Glob(filepath.Join(r.dir, "people", "*.cue"))
	for _, f := range files {
		data, err := readCUEFile(cctx, f)
		if err != nil {
			log.Printf("sync: skip %s: %v", f, err)
			continue
		}
		var p personCUE
		if err := json.Unmarshal(data, &p); err != nil {
			log.Printf("sync: skip %s: %v", f, err)
			continue
		}

		// Detect sibling image file (e.g., james-comey.jpg alongside james-comey.cue).
		// Only process if photo_url is not already set to an external URL.
		if r.imageDir != "" && !strings.HasPrefix(p.PhotoURL, "http") {
			if imgPath := findSiblingImage(f); imgPath != "" {
				photoURL, err := r.processPersonImage(tlID, tlSlug, p.Slug, imgPath)
				if err != nil {
					log.Printf("sync: image for %s: %v", p.Slug, err)
				} else {
					p.PhotoURL = photoURL
				}
			}
		}

		person, err := r.queries.UpsertPerson(r.ctx, db.UpsertPersonParams{
			TimelineID: tlID,
			Slug:       p.Slug,
			Name:       p.Name,
			SortName:   p.SortName,
			Bio:        p.Bio,
			BornDate:   parseDate(p.BornDate),
			DiedDate:   parseDate(p.DiedDate),
			PhotoUrl:   p.PhotoURL,
		})
		if err != nil {
			log.Printf("sync: person %s: %v", p.Slug, err)
			continue
		}
		personMap[p.Slug] = person.ID
		r.stats.People++
	}
	return personMap, nil
}

// findSiblingImage looks for a .jpg or .png file with the same basename as the CUE file.
func findSiblingImage(cuePath string) string {
	base := strings.TrimSuffix(cuePath, filepath.Ext(cuePath))
	for _, ext := range []string{".jpg", ".jpeg", ".png"} {
		candidate := base + ext
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// processPersonImage processes a portrait image and returns the URL path to serve it.
func (r *Runner) processPersonImage(tlID int64, tlSlug, personSlug, imgPath string) (string, error) {
	f, err := os.Open(imgPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	outDir := filepath.Join(r.imageDir, tlSlug, personSlug)
	result, err := images.ProcessPortrait(f, outDir)
	if err != nil {
		return "", fmt.Errorf("process image: %w", err)
	}

	_, err = r.queries.UpsertImage(r.ctx, db.UpsertImageParams{
		TimelineID:  tlID,
		Slug:        personSlug,
		Usage:       "portrait",
		ContentType: result.ContentType,
		Width:       int32(result.Width),
		Height:      int32(result.Height),
		FileSize:    int32(result.FileSize),
	})
	if err != nil {
		return "", fmt.Errorf("upsert image record: %w", err)
	}

	r.stats.Images++
	return fmt.Sprintf("/images/%s/%s/profile.jpg", tlSlug, personSlug), nil
}

// ---- Events ----

type eventCUE struct {
	Slug          string           `json:"slug"`
	Title         string           `json:"title"`
	Summary       string           `json:"summary"`
	Article       string           `json:"article"`
	Date          string           `json:"date"`
	EndDate       string           `json:"end_date"`
	DatePrecision string           `json:"date_precision"`
	SortOrder     int32            `json:"sort_order"`
	Category      string           `json:"category"`
	Tags          []string         `json:"tags"`
	Sources       []sourceCUE      `json:"sources"`
	People        []eventPersonCUE `json:"people"`
	Links         []eventLinkCUE   `json:"links"`
}

type sourceCUE struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	Citation string `json:"citation"`
}

type eventPersonCUE struct {
	Slug      string `json:"slug"`
	Role      string `json:"role"`
	Explainer string `json:"explainer"`
}

type eventLinkCUE struct {
	Slug         string `json:"slug"`
	Relationship string `json:"relationship"`
	Description  string `json:"description"`
}

func (r *Runner) syncEvents(cctx *cue.Context, tlID int64, catMap, personMap map[string]int64) error {
	// First pass: upsert all events, collect slug->id mapping
	eventMap := map[string]int64{}
	type pendingEvent struct {
		file string
		cue  eventCUE
		id   int64
	}
	var pending []pendingEvent

	files, _ := filepath.Glob(filepath.Join(r.dir, "events", "*.cue"))
	for _, f := range files {
		data, err := readCUEFile(cctx, f)
		if err != nil {
			log.Printf("sync: skip %s: %v", f, err)
			continue
		}
		var e eventCUE
		if err := json.Unmarshal(data, &e); err != nil {
			log.Printf("sync: skip %s: %v", f, err)
			continue
		}
		if e.DatePrecision == "" {
			e.DatePrecision = "day"
		}

		var catID pgtype.Int8
		if e.Category != "" {
			if id, ok := catMap[e.Category]; ok {
				catID = pgtype.Int8{Int64: id, Valid: true}
			}
		}

		evt, err := r.queries.UpsertEvent(r.ctx, db.UpsertEventParams{
			TimelineID:    tlID,
			Slug:          e.Slug,
			Title:         e.Title,
			Summary:       e.Summary,
			Article:       e.Article,
			EventDate:     parseDate(e.Date),
			EventEndDate:  parseDate(e.EndDate),
			DatePrecision: e.DatePrecision,
			SortOrder:     e.SortOrder,
			CategoryID:    catID,
		})
		if err != nil {
			log.Printf("sync: event %s: %v", e.Slug, err)
			continue
		}
		eventMap[e.Slug] = evt.ID
		pending = append(pending, pendingEvent{f, e, evt.ID})
		r.stats.Events++
	}

	// Second pass: sync tags, sources, people, links
	for _, pe := range pending {
		// Tags
		for _, tagSlug := range pe.cue.Tags {
			tag, err := r.queries.GetOrCreateTag(r.ctx, db.GetOrCreateTagParams{
				Slug: tagSlug,
				Name: strings.ReplaceAll(tagSlug, "-", " "),
			})
			if err != nil {
				continue
			}
			r.queries.AddEventTag(r.ctx, db.AddEventTagParams{
				EventID: pe.id,
				TagID:   tag.ID,
			})
			r.stats.Tags++
		}

		// Sources — additive (don't remove existing sources not in CUE)
		for i, src := range pe.cue.Sources {
			r.queries.AddEventSource(r.ctx, db.AddEventSourceParams{
				EventID:   pe.id,
				Url:       src.URL,
				Title:     src.Title,
				Author:    src.Author,
				Citation:  src.Citation,
				SortOrder: int32(i),
			})
			r.stats.Sources++
		}

		// People links — additive
		for i, ep := range pe.cue.People {
			if pid, ok := personMap[ep.Slug]; ok {
				r.queries.AddEventPerson(r.ctx, db.AddEventPersonParams{
					EventID:   pe.id,
					PersonID:  pid,
					Role:      ep.Role,
					Explainer: ep.Explainer,
					SortOrder: int32(i),
				})
			}
		}

		// Event links — resolved in this pass since all events are now in eventMap
		for _, link := range pe.cue.Links {
			if targetID, ok := eventMap[link.Slug]; ok {
				a, b := pe.id, targetID
				if a > b {
					a, b = b, a
				}
				r.queries.AddEventLink(r.ctx, db.AddEventLinkParams{
					EventAID:     a,
					EventBID:     b,
					Relationship: link.Relationship,
					Description:  link.Description,
				})
				r.stats.Links++
			}
		}
	}

	return nil
}

// ---- Helpers ----

func readCUEFile(cctx *cue.Context, path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	v := cctx.CompileBytes(raw, cue.Filename(path))
	if v.Err() != nil {
		return nil, fmt.Errorf("compile %s: %w", path, v.Err())
	}
	return v.MarshalJSON()
}

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
