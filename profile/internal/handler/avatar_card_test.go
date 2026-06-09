package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infodancer/web/profile/store"
)

// postAvatar sends a multipart avatar upload with same-origin headers.
func postAvatar(t *testing.T, h http.Handler, fields map[string]string, file []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if file != nil {
		fw, err := mw.CreateFormFile("avatar", "avatar.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(file); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, basePath+"/avatar", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", "http://"+req.Host)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func withIngest(fn IngestAvatar) func(*Deps) {
	return func(d *Deps) { d.IngestAvatar = fn }
}

func TestAvatarUploadDisabledWithoutIngest(t *testing.T) {
	h := newServer(t, newFakeStore(), alice)
	if rec := postAvatar(t, h, nil, []byte("img")); rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rec.Code)
	}
}

func TestAvatarUploadStoresCanonicalURL(t *testing.T) {
	fs := newFakeStore()
	var gotSubject string
	h := newServer(t, fs, alice, withIngest(func(_ context.Context, subject string, r io.Reader) (string, error) {
		gotSubject = subject
		if _, err := io.ReadAll(r); err != nil {
			return "", err
		}
		return "/media/avatar/abc123.webp", nil
	}))

	rec := postAvatar(t, h, nil, []byte("fake-image-bytes"))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if gotSubject != "u1" {
		t.Errorf("ingest subject = %q, want user key", gotSubject)
	}
	// Upload before any form save: the profile row is created on demand.
	p, err := fs.Get(context.Background(), "u1")
	if err != nil {
		t.Fatalf("profile row not created: %v", err)
	}
	if p.AvatarURL != "/media/avatar/abc123.webp" {
		t.Errorf("avatar url = %q", p.AvatarURL)
	}
	if p.DisplayName != "Alice Example" {
		t.Errorf("on-demand row did not seed display name: %q", p.DisplayName)
	}
}

func TestAvatarUploadRejectionIs422(t *testing.T) {
	h := newServer(t, newFakeStore(), alice, withIngest(func(context.Context, string, io.Reader) (string, error) {
		return "", errors.New("image too large")
	}))
	if rec := postAvatar(t, h, nil, []byte("huge")); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("got %d, want 422", rec.Code)
	}
}

func TestAvatarRemove(t *testing.T) {
	fs := newFakeStore()
	if _, err := fs.Upsert(context.Background(), store.UpsertParams{UserKey: "u1"}); err != nil {
		t.Fatal(err)
	}
	if err := fs.SetAvatarURL(context.Background(), "u1", "/media/avatar/old.webp"); err != nil {
		t.Fatal(err)
	}
	h := newServer(t, fs, alice, withIngest(func(context.Context, string, io.Reader) (string, error) {
		t.Error("ingest called on remove")
		return "", nil
	}))
	if rec := postAvatar(t, h, map[string]string{"action": "remove"}, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("got %d", rec.Code)
	}
	if p, _ := fs.Get(context.Background(), "u1"); p.AvatarURL != "" {
		t.Errorf("avatar not cleared: %q", p.AvatarURL)
	}
}

func TestAvatarUploadRequiresUser(t *testing.T) {
	h := newServer(t, newFakeStore(), nil, withIngest(func(context.Context, string, io.Reader) (string, error) {
		return "/x", nil
	}))
	if rec := postAvatar(t, h, nil, []byte("img")); rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rec.Code)
	}
}

func TestCardRendersEscapedAndLinked(t *testing.T) {
	fs := newFakeStore()
	ctx := context.Background()
	if _, err := fs.Upsert(ctx, store.UpsertParams{
		UserKey:     "u1",
		DisplayName: `Alice <script>"&`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fs.ClaimHandle(ctx, "u1", "alice"); err != nil {
		t.Fatal(err)
	}

	s, err := New(Deps{Store: fs, Resolver: stubResolver{}, BasePath: basePath})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Card(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	html := string(out)
	if strings.Contains(html, "<script>") {
		t.Error("display name not escaped in card")
	}
	for _, want := range []string{basePath + "/alice", basePath + "/avatar/alice", "profile-card"} {
		if !strings.Contains(html, want) {
			t.Errorf("card missing %q in %s", want, html)
		}
	}
}

func TestCardHandlelessProfileHasNoLink(t *testing.T) {
	fs := newFakeStore()
	ctx := context.Background()
	if _, err := fs.Upsert(ctx, store.UpsertParams{UserKey: "u1", DisplayName: "Alice"}); err != nil {
		t.Fatal(err)
	}
	s, err := New(Deps{Store: fs, Resolver: stubResolver{}, BasePath: basePath})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Card(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "<a ") {
		t.Errorf("handleless card should not link: %s", out)
	}
	if _, err := s.Card(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing profile: err = %v, want ErrNotFound", err)
	}
}
