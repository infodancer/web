package handler

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/infodancer/web/blog/bloguser"
	"github.com/infodancer/web/blog/store"
)

var (
	authorUser = &bloguser.User{Key: "u1", DisplayName: "Alice Author", Author: true}
	otherUser  = &bloguser.User{Key: "u2", DisplayName: "Bob Other", Author: true}
	adminUser  = &bloguser.User{Key: "u9", DisplayName: "Ada Admin", Admin: true}
	plainUser  = &bloguser.User{Key: "u3", DisplayName: "Carol Reader"}
)

func get(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	return w
}

func doPost(t *testing.T, srv *Server, path string, form url.Values, sameOrigin bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if sameOrigin {
		req.Header.Set("Origin", "http://"+req.Host)
	}
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	return w
}

func TestIndexListsPublishedWithAutodiscovery(t *testing.T) {
	env := newTestServer(t, nil)
	env.seedPost(t, "u1", store.StatusPublished, "First Light", nil, "osr")
	env.seedPost(t, "u1", store.StatusDraft, "Hidden Draft", nil)

	w := get(t, env.server, "/")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "First Light") {
		t.Error("published post missing from index")
	}
	if strings.Contains(body, "Hidden Draft") {
		t.Error("draft leaked into index")
	}
	for _, want := range []string{
		`type="application/rss+xml"`,
		`type="application/atom+xml"`,
		`type="application/feed+json"`,
		`href="https://example.org/blog/feed.xml"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("autodiscovery missing %q", want)
		}
	}
	if !strings.Contains(body, `"@type":"Blog"`) {
		t.Error("Blog JSON-LD missing from index")
	}
}

func TestPostPageVisibilityAndJSONLD(t *testing.T) {
	env := newTestServer(t, nil)
	pub := env.seedPost(t, "u1", store.StatusPublished, "Shipping News", nil, "announcements")
	draft := env.seedPost(t, "u1", store.StatusDraft, "Not Yet", nil)

	w := get(t, env.server, "/p/"+pub.Slug)
	if w.Code != http.StatusOK {
		t.Fatalf("published post status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"@type":"BlogPosting"`) || !strings.Contains(body, `"headline":"Shipping News"`) {
		t.Error("BlogPosting JSON-LD missing")
	}
	if !strings.Contains(body, "<strong>world</strong>") {
		t.Error("rendered body missing")
	}

	if w := get(t, env.server, "/p/"+draft.Slug); w.Code != http.StatusNotFound {
		t.Errorf("anonymous draft fetch = %d, want 404", w.Code)
	}

	// The author sees their own draft.
	authorEnv := newTestServer(t, authorUser)
	d2 := authorEnv.seedPost(t, "u1", store.StatusDraft, "My Draft", nil)
	if w := get(t, authorEnv.server, "/p/"+d2.Slug); w.Code != http.StatusOK {
		t.Errorf("author draft fetch = %d, want 200", w.Code)
	}
}

func TestFeedMatrix(t *testing.T) {
	env := newTestServer(t, adminUser)
	b, err := env.store.CreateBlog(t.Context(), "admin-notes", "Admin Notes", "site news")
	if err != nil {
		t.Fatal(err)
	}
	env.seedPost(t, "u1", store.StatusPublished, "In The Stream", &b.ID, "meta")
	env.seedPost(t, "u2", store.StatusPublished, "Standalone Post", nil)

	cases := []struct {
		path        string
		contentType string
		contains    []string
		excludes    []string
	}{
		{"/feed.xml", "application/rss+xml", []string{"In The Stream", "Standalone Post", "<rss"}, nil},
		{"/feed.atom", "application/atom+xml", []string{"In The Stream", "Standalone Post", "<feed"}, nil},
		{"/feed.json", "application/feed+json", []string{"In The Stream", "https://jsonfeed.org/version/1.1"}, nil},
		{"/b/admin-notes/feed.xml", "application/rss+xml", []string{"In The Stream"}, []string{"Standalone Post"}},
		{"/b/admin-notes/feed.atom", "application/atom+xml", []string{"In The Stream"}, []string{"Standalone Post"}},
		{"/b/admin-notes/feed.json", "application/feed+json", []string{"In The Stream"}, []string{"Standalone Post"}},
		{"/authors/author-u2/feed.xml", "application/rss+xml", []string{"Standalone Post"}, []string{"In The Stream"}},
		{"/authors/author-u2/feed.atom", "application/atom+xml", []string{"Standalone Post"}, []string{"In The Stream"}},
		{"/authors/author-u2/feed.json", "application/feed+json", []string{"Standalone Post"}, []string{"In The Stream"}},
	}
	for _, c := range cases {
		w := get(t, env.server, c.path)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d", c.path, w.Code)
			continue
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, c.contentType) {
			t.Errorf("%s: content type %q, want %q", c.path, ct, c.contentType)
		}
		body := w.Body.String()
		for _, want := range c.contains {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %q", c.path, want)
			}
		}
		for _, no := range c.excludes {
			if strings.Contains(body, no) {
				t.Errorf("%s: unexpectedly contains %q", c.path, no)
			}
		}
	}

	// Tags surface as categories in the scoped RSS feed.
	w := get(t, env.server, "/b/admin-notes/feed.xml")
	if !strings.Contains(w.Body.String(), "<category>meta</category>") {
		t.Error("rss feed missing tag category")
	}

	// Unknown scopes 404.
	if w := get(t, env.server, "/b/nope/feed.xml"); w.Code != http.StatusNotFound {
		t.Errorf("unknown blog feed = %d, want 404", w.Code)
	}
	if w := get(t, env.server, "/authors/nope/feed.json"); w.Code != http.StatusNotFound {
		t.Errorf("unknown author feed = %d, want 404", w.Code)
	}
}

func TestSitemap(t *testing.T) {
	env := newTestServer(t, nil)
	p := env.seedPost(t, "u1", store.StatusPublished, "Mapped", nil, "osr")

	w := get(t, env.server, "/sitemap.xml")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var parsed struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("re-parse sitemap: %v", err)
	}
	wantLocs := []string{
		"https://example.org/blog/",
		"https://example.org/blog/p/" + p.Slug,
		"https://example.org/blog/authors/author-u1",
		"https://example.org/blog/tags/osr",
	}
	for _, want := range wantLocs {
		found := false
		for _, u := range parsed.URLs {
			if u.Loc == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("sitemap missing %s", want)
		}
	}
}

func TestWriteRequiresRole(t *testing.T) {
	anon := newTestServer(t, nil)
	if w := get(t, anon.server, "/new"); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous /new = %d, want 401", w.Code)
	}
	reader := newTestServer(t, plainUser)
	if w := get(t, reader.server, "/new"); w.Code != http.StatusForbidden {
		t.Errorf("non-author /new = %d, want 403", w.Code)
	}
	admin := newTestServer(t, adminUser)
	if w := get(t, admin.server, "/new"); w.Code != http.StatusOK {
		t.Errorf("admin /new = %d, want 200", w.Code)
	}
}

func TestCSRFGuardOnWrites(t *testing.T) {
	env := newTestServer(t, authorUser)
	form := url.Values{"title": {"x"}, "body_md": {"y"}, "action": {"draft"}}
	if w := doPost(t, env.server, "/new", form, false); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin POST = %d, want 403", w.Code)
	}
}

func TestCreatePublishEditLifecycle(t *testing.T) {
	env := newTestServer(t, authorUser)

	// Create as draft.
	w := doPost(t, env.server, "/new", url.Values{
		"title":   {"The Plague at Bobwhite Castle"},
		"body_md": {"It began with **rats**."},
		"tags":    {"osr, Session Reports"},
		"action":  {"draft"},
	}, true)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("create = %d, want 303: %s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if loc != "/blog/p/the-plague-at-bobwhite-castle" {
		t.Fatalf("redirect = %q", loc)
	}
	post, err := env.store.GetPostBySlug(t.Context(), "the-plague-at-bobwhite-castle")
	if err != nil {
		t.Fatalf("post not stored: %v", err)
	}
	if post.Status != store.StatusDraft {
		t.Errorf("status = %q, want draft", post.Status)
	}
	if !strings.Contains(post.BodyHTML, "<strong>rats</strong>") {
		t.Errorf("body not rendered: %q", post.BodyHTML)
	}
	if len(post.Tags) != 2 || post.Tags[1].Slug != "session-reports" {
		t.Errorf("tags = %+v", post.Tags)
	}
	if len(env.indexer.indexed) != 0 {
		t.Error("draft was indexed")
	}

	// Publish.
	id := strconv.FormatInt(post.ID, 10)
	if w := doPost(t, env.server, "/p/"+id+"/publish", url.Values{}, true); w.Code != http.StatusSeeOther {
		t.Fatalf("publish = %d", w.Code)
	}
	post, _ = env.store.GetPostByID(t.Context(), post.ID)
	if post.Status != store.StatusPublished || post.PublishedAt == nil {
		t.Fatalf("after publish: status=%q publishedAt=%v", post.Status, post.PublishedAt)
	}
	doc, ok := env.indexer.indexed[id]
	if !ok {
		t.Fatal("publish did not index")
	}
	if doc.URL != "https://example.org/blog/p/"+post.Slug {
		t.Errorf("indexed URL = %q", doc.URL)
	}
	if len(doc.Tags) != 2 {
		t.Errorf("indexed tags = %v", doc.Tags)
	}

	// Edit while published: stays published, re-indexed with new title.
	w = doPost(t, env.server, "/p/"+id+"/edit", url.Values{
		"title":   {"The Plague, Revisited"},
		"body_md": {"Updated body."},
		"action":  {"publish"},
	}, true)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("edit = %d: %s", w.Code, w.Body.String())
	}
	post, _ = env.store.GetPostByID(t.Context(), post.ID)
	if post.Slug != "the-plague-at-bobwhite-castle" {
		t.Errorf("slug changed on rename: %q", post.Slug)
	}
	if env.indexer.indexed[id].Title != "The Plague, Revisited" {
		t.Errorf("index not refreshed: %+v", env.indexer.indexed[id])
	}

	// Unpublish removes from the index.
	if w := doPost(t, env.server, "/p/"+id+"/unpublish", url.Values{}, true); w.Code != http.StatusSeeOther {
		t.Fatalf("unpublish = %d", w.Code)
	}
	if _, ok := env.indexer.indexed[id]; ok {
		t.Error("unpublish left the post indexed")
	}

	// Delete.
	if w := doPost(t, env.server, "/p/"+id+"/delete", url.Values{}, true); w.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d", w.Code)
	}
	if _, err := env.store.GetPostByID(t.Context(), post.ID); err == nil {
		t.Error("post still readable after delete")
	}
}

func TestCreateValidation(t *testing.T) {
	env := newTestServer(t, authorUser)
	w := doPost(t, env.server, "/new", url.Values{
		"title":   {""},
		"body_md": {""},
		"action":  {"draft"},
	}, true)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
	if !strings.Contains(w.Body.String(), "a title is required") {
		t.Error("validation message missing")
	}
}

func TestEditForeignPostForbidden(t *testing.T) {
	env := newTestServer(t, otherUser)
	p := env.seedPost(t, "u1", store.StatusPublished, "Not Yours", nil)
	id := strconv.FormatInt(p.ID, 10)

	w := doPost(t, env.server, "/p/"+id+"/edit", url.Values{
		"title": {"Hijacked"}, "body_md": {"x"}, "action": {"draft"},
	}, true)
	if w.Code != http.StatusForbidden {
		t.Errorf("foreign edit = %d, want 403", w.Code)
	}

	// Admin may edit anyone's post.
	adminEnv := newTestServer(t, adminUser)
	p2 := adminEnv.seedPost(t, "u1", store.StatusPublished, "Theirs", nil)
	id2 := strconv.FormatInt(p2.ID, 10)
	w = doPost(t, adminEnv.server, "/p/"+id2+"/edit", url.Values{
		"title": {"Edited by admin"}, "body_md": {"x"}, "action": {"publish"},
	}, true)
	if w.Code != http.StatusSeeOther {
		t.Errorf("admin edit = %d, want 303: %s", w.Code, w.Body.String())
	}
}

func TestUpload(t *testing.T) {
	env := newTestServer(t, authorUser)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("image", "pic.png")
	_, _ = fw.Write([]byte("fake-image-bytes"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", "http://"+req.Host)
	w := httptest.NewRecorder()
	env.server.Routes().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("upload = %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if resp["url"] != "/media/blog/abc123.webp" {
		t.Errorf("url = %q", resp["url"])
	}

	// Anonymous upload is rejected.
	anon := newTestServer(t, nil)
	req2 := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(""))
	req2.Header.Set("Origin", "http://"+req2.Host)
	w2 := httptest.NewRecorder()
	anon.server.Routes().ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("anonymous upload = %d, want 401", w2.Code)
	}
}

func TestAdminBlogManagement(t *testing.T) {
	// Non-admin author is refused.
	authorEnv := newTestServer(t, authorUser)
	if w := get(t, authorEnv.server, "/admin/blogs"); w.Code != http.StatusForbidden {
		t.Errorf("author /admin/blogs = %d, want 403", w.Code)
	}

	env := newTestServer(t, adminUser)
	w := doPost(t, env.server, "/admin/blogs", url.Values{
		"name": {"The Weekly Owlbear"}, "description": {"humor column"},
	}, true)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("create blog = %d: %s", w.Code, w.Body.String())
	}
	b, err := env.store.GetBlogBySlug(t.Context(), "the-weekly-owlbear")
	if err != nil {
		t.Fatalf("blog not stored: %v", err)
	}

	// Duplicate slug is a validation error, not a 500.
	w = doPost(t, env.server, "/admin/blogs", url.Values{
		"name": {"The Weekly Owlbear"},
	}, true)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("duplicate blog = %d, want 422", w.Code)
	}

	// Delete refuses while posts reference it.
	env.seedPost(t, "u1", store.StatusPublished, "In Stream", &b.ID)
	id := strconv.FormatInt(b.ID, 10)
	w = doPost(t, env.server, "/admin/blogs/"+id+"/delete", url.Values{}, true)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("delete in-use blog = %d, want 422", w.Code)
	}
}

func TestBlogAndAuthorAndTagPages(t *testing.T) {
	env := newTestServer(t, adminUser)
	b, _ := env.store.CreateBlog(t.Context(), "admin-notes", "Admin Notes", "news")
	env.seedPost(t, "u1", store.StatusPublished, "Streamed", &b.ID, "meta")

	w := get(t, env.server, "/b/admin-notes")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Streamed") {
		t.Errorf("blog page: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `href="https://example.org/blog/b/admin-notes/feed.atom"`) {
		t.Error("blog page missing scoped autodiscovery")
	}

	w = get(t, env.server, "/authors")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Author U1") {
		t.Errorf("authors page: %d", w.Code)
	}
	w = get(t, env.server, "/authors/author-u1")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Streamed") {
		t.Errorf("author page: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"@type":"ProfilePage"`) {
		t.Error("author page missing ProfilePage JSON-LD")
	}

	w = get(t, env.server, "/tags")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "meta") {
		t.Errorf("tags page: %d", w.Code)
	}
	w = get(t, env.server, "/tags/meta")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Streamed") {
		t.Errorf("tag page: %d", w.Code)
	}

	if w := get(t, env.server, "/b/missing"); w.Code != http.StatusNotFound {
		t.Errorf("missing blog = %d", w.Code)
	}
}

func TestEditFormShowsEditorWithUpload(t *testing.T) {
	env := newTestServer(t, authorUser)
	w := get(t, env.server, "/new")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"data-mdedit",
		`data-mdedit-upload="/blog/upload"`,
		"/static/mdedit/mdedit.js",
		`name="body_md"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form missing %q", want)
		}
	}
}

func TestReindexAll(t *testing.T) {
	env := newTestServer(t, nil)
	p1 := env.seedPost(t, "u1", store.StatusPublished, "One", nil)
	env.seedPost(t, "u1", store.StatusDraft, "Two", nil)

	if err := env.server.ReindexAll(t.Context()); err != nil {
		t.Fatalf("ReindexAll: %v", err)
	}
	if len(env.indexer.indexed) != 1 {
		t.Fatalf("indexed %d docs, want 1", len(env.indexer.indexed))
	}
	if _, ok := env.indexer.indexed[strconv.FormatInt(p1.ID, 10)]; !ok {
		t.Error("published post not in index")
	}
}
