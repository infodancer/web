package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/infodancer/web/profile/store"
)

// seedProfile stores a complete profile under handle "alice".
func seedProfile(t *testing.T, fs *fakeStore) {
	t.Helper()
	ctx := context.Background()
	if _, err := fs.Upsert(ctx, store.UpsertParams{
		UserKey:     "u1",
		DisplayName: "Alice",
		BioMD:       "Likes *dice* and <script>alert(1)</script>",
		Links:       []store.Link{{Label: "Blog", URL: "https://example.org/blog"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := fs.ClaimHandle(ctx, "u1", "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestPublicProfileRenders(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, nil) // anonymous: public page needs no user

	rec := do(t, h, http.MethodGet, basePath+"/alice", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Alice", "@alice", "<em>dice</em>", `rel="me nofollow ugc"`, "https://example.org/blog"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestPublicProfileSanitizesBio(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, nil)

	body := do(t, h, http.MethodGet, basePath+"/alice", nil, nil).Body.String()
	if strings.Contains(body, "<script>") {
		t.Error("bio script tag survived sanitization")
	}
}

func TestPublicProfileCaseInsensitiveLookup(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, nil)
	if rec := do(t, h, http.MethodGet, basePath+"/ALICE", nil, nil); rec.Code != http.StatusOK {
		t.Errorf("uppercase handle lookup: got %d, want 200", rec.Code)
	}
}

func TestUnknownAndMalformedHandles404(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, nil)
	for _, target := range []string{"/nobody-here", "/a", "/" + strings.Repeat("x", 40), "/has_underscore", "/-leading"} {
		if rec := do(t, h, http.MethodGet, basePath+target, nil, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", target, rec.Code)
		}
	}
}

func TestIdenticonServed(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, nil)

	rec := do(t, h, http.MethodGet, basePath+"/avatar/alice", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("Content-Type = %q", ct)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); csp != "default-src 'none'" {
		t.Errorf("CSP = %q", csp)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Error("no SVG in body")
	}
	if rec := do(t, h, http.MethodGet, basePath+"/avatar/nobody", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unclaimed handle identicon: got %d, want 404", rec.Code)
	}
}

func TestProfilePageFallsBackToIdenticon(t *testing.T) {
	fs := newFakeStore()
	seedProfile(t, fs)
	h := newServer(t, fs, nil)
	body := do(t, h, http.MethodGet, basePath+"/alice", nil, nil).Body.String()
	if !strings.Contains(body, basePath+"/avatar/alice") {
		t.Error("profile page does not reference the identicon fallback")
	}
}
