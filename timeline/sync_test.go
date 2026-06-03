package timeline_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infodancer/web/media"
	"github.com/infodancer/web/timeline"
	"github.com/infodancer/web/timeline/timelineuser"
)

// writeCUETree builds a minimal CUE directory tree (one category, one person
// with a sibling portrait, one event) under dir.
func writeCUETree(t *testing.T, dir string) {
	t.Helper()
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("timeline.cue", `name: "Synced"`+"\n"+`slug: "synced"`+"\n"+`description: "from cue"`+"\n")
	write("categories/politics.cue", `slug: "politics"`+"\n"+`name: "Politics"`+"\n")
	write("people/ada.cue", `slug: "ada"`+"\n"+`name: "Ada Lovelace"`+"\n")
	write("events/e1.cue", `slug: "e1"`+"\n"+`title: "First"`+"\n"+`date: "2001-01-01"`+"\n"+`category: "politics"`+"\n"+`article: "## Body"`+"\n"+`people: [{slug: "ada", role: "subject"}]`+"\n")

	// A sibling portrait beside ada.cue exercises the media blob path.
	img := image.NewRGBA(image.Rect(0, 0, 80, 80))
	for y := range 80 {
		for x := range 80 {
			img.Set(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 3), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode portrait: %v", err)
	}
	write("people/ada.jpg", buf.String())
}

// TestModuleSync exercises the re-homed CUE directory sync (Module.Sync),
// including portrait storage through the media blob store.
func TestModuleSync(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)

	blobs := media.NewMemoryBlobStore()
	m, err := timeline.New(timeline.Config{
		DB:          pool,
		Resolver:    resolver{u: &timelineuser.User{Subject: "s", Issuer: "i", Admin: true}},
		BasePath:    basePath,
		Blobs:       blobs,
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	// Clean slate now that tables exist.
	if _, err := pool.Exec(ctx, "TRUNCATE timeline_timelines, timeline_events, timeline_categories, timeline_tags, timeline_people, timeline_event_people, timeline_users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	dir := t.TempDir()
	writeCUETree(t, dir)

	stats, err := m.Sync(ctx, dir)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if stats.Categories != 1 || stats.People != 1 || stats.Events != 1 {
		t.Fatalf("sync stats: got %+v, want 1/1/1 cat/people/events", stats)
	}
	if stats.Images != 1 {
		t.Errorf("sync images: got %d, want 1 (sibling portrait stored)", stats.Images)
	}

	// The synced timeline is now servable.
	if _, code := get(t, mount(m), basePath+"/t/synced"); code != 200 {
		t.Errorf("synced timeline page: code=%d, want 200", code)
	}

	// The person page links a portrait under the module's /portrait/ route, and
	// the blob is retrievable from the media store.
	body, code := get(t, mount(m), basePath+"/t/synced/person/ada")
	if code != 200 {
		t.Fatalf("person page: code=%d, want 200", code)
	}
	if !strings.Contains(body, basePath+"/portrait/") {
		t.Errorf("person page does not reference a /portrait/ URL; portrait not stored")
	}
}
