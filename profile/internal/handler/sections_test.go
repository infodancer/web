package handler

import (
	"context"
	"html/template"
	"net/http"
	"strings"
	"testing"

	"github.com/infodancer/web/profile/profileuser"
)

// sectionDeps installs a Sections hook that records its arguments and
// returns the given sections.
func sectionDeps(secs []Section, gotKeys *[]string) func(*Deps) {
	return func(d *Deps) {
		d.Sections = func(_ context.Context, _ *http.Request, userKey string) []Section {
			if gotKeys != nil {
				*gotKeys = append(*gotKeys, userKey)
			}
			return secs
		}
	}
}

func TestSectionsRenderOnPublicPage(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	var keys []string
	h := newServer(t, fs, nil, sectionDeps([]Section{
		{Title: "Gaming", ViewHTML: `<p class="gaming-view">Near Springfield</p>`, EditHTML: `<form class="gaming-edit"></form>`},
		{Title: "Edit only", EditHTML: `<p class="edit-only">hidden</p>`},
	}, &keys))

	rec := do(t, h, http.MethodGet, basePath+"/alice", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`<h2>Gaming</h2>`, `class="gaming-view"`, `class="profile-section"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	for _, never := range []string{`class="gaming-edit"`, "Edit only", `class="edit-only"`} {
		if strings.Contains(body, never) {
			t.Errorf("page leaked edit-side markup %q", never)
		}
	}
	// The hook receives the profile owner's key, not the viewer's.
	if len(keys) != 1 || keys[0] != "u1" {
		t.Errorf("hook keys = %v, want [u1]", keys)
	}
}

func TestSectionsRenderOnEditPage(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	var keys []string
	h := newServer(t, fs, &profileuser.User{Key: "u1", DisplayName: "Alice"}, sectionDeps([]Section{
		{Title: "Gaming", ViewHTML: `<p class="gaming-view">Near Springfield</p>`, EditHTML: `<form class="gaming-edit"></form>`},
		{Title: "View only", ViewHTML: `<p class="view-only">hidden</p>`},
	}, &keys))

	rec := do(t, h, http.MethodGet, basePath+"/edit", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`<h2>Gaming</h2>`, `class="gaming-edit"`} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
	for _, never := range []string{`class="gaming-view"`, "View only"} {
		if strings.Contains(body, never) {
			t.Errorf("edit page leaked view-side markup %q", never)
		}
	}
	if len(keys) != 1 || keys[0] != "u1" {
		t.Errorf("hook keys = %v, want [u1]", keys)
	}
}

func TestSectionsHTMLIsNotReescaped(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, nil, sectionDeps([]Section{
		{Title: "Raw", ViewHTML: template.HTML(`<a href="/x">link</a>`)},
	}, nil))

	body := do(t, h, http.MethodGet, basePath+"/alice", nil, nil).Body.String()
	if !strings.Contains(body, `<a href="/x">link</a>`) {
		t.Error("section template.HTML was escaped or dropped")
	}
}

func TestNilSectionsHookRendersNothing(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, &profileuser.User{Key: "u1"})

	for _, target := range []string{basePath + "/alice", basePath + "/edit"} {
		rec := do(t, h, http.MethodGet, target, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: got %d, want 200", target, rec.Code)
		}
	}
}
