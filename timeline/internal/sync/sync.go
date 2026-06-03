// Package sync reads a CUE directory tree and upserts timeline data into the
// database.
//
// Directory structure:
//
//	mydata/
//	  timeline.cue          # timeline metadata
//	  categories/*.cue      # one file per category
//	  people/*.cue          # one file per person
//	  events/*.cue          # one file per event
//
// Schema-owned fields are updated on conflict. DB-only content (e.g. data added
// via the web UI) is never touched. Sibling portrait images are stored through
// the media blob store.
package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/infodancer/web/media"
	"github.com/infodancer/web/timeline/db"
	"github.com/infodancer/web/timeline/internal/images"
)

// portraitCategory is the media bucket portraits are stored under. It must
// match the constant of the same name in the handler package, which serves
// them.
const portraitCategory = "timeline-portrait"

// Runner holds the dependencies for a sync run.
type Runner struct {
	queries  *db.Queries
	blobs    media.BlobStore // optional; nil disables portrait ingestion
	objects  *media.Store    // optional; portrait metadata
	basePath string          // mount prefix, used to build portrait URLs
	ctx      context.Context
	dir      string
	stats    Stats
}

// Stats reports what a sync run touched.
type Stats struct {
	Categories int
	People     int
	Events     int
	Tags       int
	Links      int
	Sources    int
	Images     int
}

// New constructs a Runner over the given queries and optional media stores.
func New(queries *db.Queries, blobs media.BlobStore, objects *media.Store, basePath string) *Runner {
	return &Runner{queries: queries, blobs: blobs, objects: objects, basePath: basePath}
}

// Run reads the CUE tree at dir and upserts it.
func (r *Runner) Run(ctx context.Context, dir string) (Stats, error) {
	r.ctx = ctx
	r.dir = dir
	cctx := cuecontext.New()

	tl, err := r.syncTimeline(cctx)
	if err != nil {
		return r.stats, fmt.Errorf("sync timeline: %w", err)
	}
	log.Printf("sync: timeline %q (id=%d)", tl.Slug, tl.ID)

	catMap, err := r.syncCategories(cctx, tl.ID)
	if err != nil {
		return r.stats, fmt.Errorf("sync categories: %w", err)
	}

	personMap, err := r.syncPeople(cctx, tl.ID)
	if err != nil {
		return r.stats, fmt.Errorf("sync people: %w", err)
	}

	if err := r.syncEvents(cctx, tl.ID, catMap, personMap); err != nil {
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

func (r *Runner) syncTimeline(cctx *cue.Context) (db.TimelineTimeline, error) {
	data, err := readCUEFile(cctx, filepath.Join(r.dir, "timeline.cue"))
	if err != nil {
		return db.TimelineTimeline{}, err
	}
	var t timelineCUE
	if err := json.Unmarshal(data, &t); err != nil {
		return db.TimelineTimeline{}, fmt.Errorf("decode timeline.cue: %w", err)
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

func (r *Runner) syncPeople(cctx *cue.Context, tlID int64) (map[string]int64, error) {
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

		// A sibling image file (e.g. james-comey.jpg beside james-comey.cue) is
		// stored through the media blob store, unless photo_url already points
		// at an external URL.
		if r.blobs != nil && !strings.HasPrefix(p.PhotoURL, "http") {
			if imgPath := findSiblingImage(f); imgPath != "" {
				photoURL, err := r.processPersonImage(tlID, p.Slug, imgPath)
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

// findSiblingImage looks for a .jpg/.jpeg/.png file sharing the CUE file's basename.
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

// processPersonImage produces a square portrait, stores its bytes in the media
// blob store keyed by content hash, records metadata, and returns the URL the
// handler serves it from.
func (r *Runner) processPersonImage(tlID int64, personSlug, imgPath string) (string, error) {
	f, err := os.Open(imgPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	data, contentType, width, height, err := images.PortraitBytes(f)
	if err != nil {
		return "", fmt.Errorf("process image: %w", err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	if err := r.blobs.Put(r.ctx, portraitCategory, hash, contentType, data); err != nil {
		return "", fmt.Errorf("store portrait blob: %w", err)
	}
	if r.objects != nil {
		if err := r.objects.Put(r.ctx, media.ObjectMeta{
			Category:    portraitCategory,
			Hash:        hash,
			ContentType: contentType,
			Width:       width,
			Height:      height,
			ByteSize:    int64(len(data)),
			Subject:     fmt.Sprintf("%d/%s", tlID, personSlug),
			CreatedAt:   time.Now(),
		}); err != nil {
			return "", fmt.Errorf("record portrait metadata: %w", err)
		}
	}

	r.stats.Images++
	return r.basePath + "/portrait/" + hash, nil
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
	eventMap := map[string]int64{}
	type pendingEvent struct {
		cue eventCUE
		id  int64
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
		pending = append(pending, pendingEvent{e, evt.ID})
		r.stats.Events++
	}

	for _, pe := range pending {
		for _, tagSlug := range pe.cue.Tags {
			tag, err := r.queries.GetOrCreateTag(r.ctx, db.GetOrCreateTagParams{
				Slug: tagSlug,
				Name: strings.ReplaceAll(tagSlug, "-", " "),
			})
			if err != nil {
				continue
			}
			if err := r.queries.AddEventTag(r.ctx, db.AddEventTagParams{EventID: pe.id, TagID: tag.ID}); err != nil {
				continue
			}
			r.stats.Tags++
		}

		for i, src := range pe.cue.Sources {
			if _, err := r.queries.AddEventSource(r.ctx, db.AddEventSourceParams{
				EventID:   pe.id,
				Url:       src.URL,
				Title:     src.Title,
				Author:    src.Author,
				Citation:  src.Citation,
				SortOrder: int32(i),
			}); err != nil {
				continue
			}
			r.stats.Sources++
		}

		for i, ep := range pe.cue.People {
			if pid, ok := personMap[ep.Slug]; ok {
				_ = r.queries.AddEventPerson(r.ctx, db.AddEventPersonParams{
					EventID:   pe.id,
					PersonID:  pid,
					Role:      ep.Role,
					Explainer: ep.Explainer,
					SortOrder: int32(i),
				})
			}
		}

		for _, link := range pe.cue.Links {
			if targetID, ok := eventMap[link.Slug]; ok {
				a, b := pe.id, targetID
				if a > b {
					a, b = b, a
				}
				if err := r.queries.AddEventLink(r.ctx, db.AddEventLinkParams{
					EventAID:     a,
					EventBID:     b,
					Relationship: link.Relationship,
					Description:  link.Description,
				}); err == nil {
					r.stats.Links++
				}
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
