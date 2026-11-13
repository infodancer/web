package handler

import (
	"html/template"
	"net/http"
	"strings"
	"testing"

	"github.com/infodancer/web/profile/profileuser"
)

func headerDeps(d *Deps) {
	d.EditHeader = func(*http.Request) template.HTML {
		return `<h1>Your account</h1><nav class="host-tabs">tabs</nav>`
	}
}

func TestEditHeader_TopsTheEditPage(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, &profileuser.User{Key: "u1", DisplayName: "Alice"}, headerDeps)

	rec := do(t, h, http.MethodGet, basePath+"/edit", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	hdr := strings.Index(body, `<nav class="host-tabs">tabs</nav>`)
	title := strings.Index(body, "<h2>Your profile</h2>")
	if hdr < 0 || title < hdr {
		t.Fatalf("want the host header (at %d) before an h2 title (at %d)", hdr, title)
	}
	if n := strings.Count(body, "<h1>"); n != 1 {
		t.Errorf("%d h1 elements, want 1 (the host's)", n)
	}
}

func TestEditHeader_NotOnThePublicPage(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, nil, headerDeps)

	rec := do(t, h, http.MethodGet, basePath+"/alice", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "host-tabs") {
		t.Error("the account header leaked onto the public profile")
	}
}

func TestEditHeader_UnsetKeepsTheH1(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, &profileuser.User{Key: "u1", DisplayName: "Alice"})

	rec := do(t, h, http.MethodGet, basePath+"/edit", nil, nil)
	if !strings.Contains(rec.Body.String(), "<h1>Your profile</h1>") {
		t.Error("without a header the edit page should keep its h1")
	}
}
