package handler

import (
	"net/http"
	"strings"
	"testing"
)

func TestEditPageEmbedsMdeditField(t *testing.T) {
	fs := newFakeStore()
	h := newServer(t, fs, alice)

	body := do(t, h, http.MethodGet, basePath+"/edit", nil, nil).Body.String()
	for _, want := range []string{
		`name="bio"`,                     // the field still posts as "bio"
		basePath + "/static/mdedit/",     // head tags reference the module's asset mount
		`data-mdedit-toolbar="standard"`, // comment-grade toolbar
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
}

func TestMdeditAssetsServed(t *testing.T) {
	h := newServer(t, newFakeStore(), alice)
	rec := do(t, h, http.MethodGet, basePath+"/static/mdedit/mdedit.js", nil, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("mdedit asset: got %d, want 200", rec.Code)
	}
}
