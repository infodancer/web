package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeStore is an in-memory objectStore for handler unit tests, keyed by
// (category, hash).
type fakeStore struct {
	mu      sync.Mutex
	objects map[string]ObjectMeta
	putErr  error
}

func newFakeStore() *fakeStore { return &fakeStore{objects: map[string]ObjectMeta{}} }

func (f *fakeStore) key(category, hash string) string { return category + "\x00" + hash }

func (f *fakeStore) Put(_ context.Context, m ObjectMeta) error {
	if f.putErr != nil {
		return f.putErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[f.key(m.Category, m.Hash)] = m
	return nil
}

func (f *fakeStore) Lookup(_ context.Context, category, hash string) (ObjectMeta, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.objects[f.key(category, hash)]
	return m, ok, nil
}

func newHandler(t *testing.T, cfg Config) (*Handler, *fakeStore, *MemoryBlobStore) {
	t.Helper()
	if cfg.Category == "" {
		cfg.Category = "campaign"
	}
	store := newFakeStore()
	blobs := NewMemoryBlobStore()
	h, err := NewHandler(store, blobs, cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h, store, blobs
}

// uploadRequest builds a multipart POST with the bytes in the "image" field.
func uploadRequest(t *testing.T, field string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile(field, "upload.bin")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestNewHandler_RejectsBadCategory(t *testing.T) {
	for _, bad := range []string{"", "../evil", "UPPER", "has space"} {
		if _, err := NewHandler(newFakeStore(), NewMemoryBlobStore(), Config{Category: bad}); err == nil {
			t.Errorf("NewHandler(category=%q) = nil error, want error", bad)
		}
	}
}

func TestHandler_UploadPNG(t *testing.T) {
	h, store, blobs := newHandler(t, Config{Category: "campaign", PublicBaseURL: "/media"})
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, uploadRequest(t, "image", pngBytes(t, 30, 20)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Width != 30 || resp.Height != 20 {
		t.Errorf("dims = %dx%d, want 30x20", resp.Width, resp.Height)
	}
	if !bytes.HasPrefix([]byte(resp.URL), []byte("/media/")) || !bytes.HasSuffix([]byte(resp.URL), []byte(".webp")) {
		t.Errorf("url = %q, want /media/<hash>.webp (PNG re-encodes to WebP)", resp.URL)
	}
	// Side effects: one metadata row and one blob, both under the category.
	if len(store.objects) != 1 {
		t.Errorf("stored %d objects, want 1", len(store.objects))
	}
	for _, m := range store.objects {
		if m.Category != "campaign" {
			t.Errorf("stored category = %q, want campaign", m.Category)
		}
		if ok, _ := blobs.Has(context.Background(), m.Category, m.Hash); !ok {
			t.Errorf("blob for %s/%s not stored", m.Category, m.Hash)
		}
	}
}

func TestHandler_UploadRejectsNonImage(t *testing.T) {
	h, _, _ := newHandler(t, Config{})
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, uploadRequest(t, "image", []byte("definitely not an image")))
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestHandler_UploadMissingField(t *testing.T) {
	h, _, _ := newHandler(t, Config{})
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, uploadRequest(t, "wrongfield", pngBytes(t, 8, 8)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_UploadAuthDenied(t *testing.T) {
	cfg := Config{Authorize: func(*http.Request) (string, bool) { return "", false }}
	h, store, _ := newHandler(t, cfg)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, uploadRequest(t, "image", pngBytes(t, 8, 8)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if len(store.objects) != 0 {
		t.Errorf("denied upload still stored %d objects", len(store.objects))
	}
}

func TestHandler_UploadRecordsSubject(t *testing.T) {
	cfg := Config{Authorize: func(*http.Request) (string, bool) { return "shadowmaze", true }}
	h, store, _ := newHandler(t, cfg)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, uploadRequest(t, "image", pngBytes(t, 8, 8)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	for _, m := range store.objects {
		if m.Subject != "shadowmaze" {
			t.Errorf("subject = %q, want shadowmaze", m.Subject)
		}
	}
}

func TestHandler_UploadRejectsCrossOrigin(t *testing.T) {
	h, _, _ := newHandler(t, Config{})
	req := uploadRequest(t, "image", pngBytes(t, 8, 8))
	req.Host = "example.com"
	req.Header.Set("Origin", "https://evil.test")
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestHandler_UploadStoreErrorIs500(t *testing.T) {
	h, store, _ := newHandler(t, Config{})
	store.putErr = errors.New("db down")
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, uploadRequest(t, "image", pngBytes(t, 8, 8)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// upload then serve: the round trip returns the bytes with the security headers.
func TestHandler_ServeRoundTrip(t *testing.T) {
	h, _, _ := newHandler(t, Config{Category: "campaign", PublicBaseURL: "/media"})
	up := httptest.NewRecorder()
	h.Routes().ServeHTTP(up, uploadRequest(t, "image", pngBytes(t, 12, 12)))
	var resp uploadResponse
	if err := json.Unmarshal(up.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// resp.URL is "/media/<hash>.webp"; strip the mount prefix to hit Routes.
	path := resp.URL[len("/media"):]
	get := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, get)

	if rec.Code != http.StatusOK {
		t.Fatalf("serve status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/webp" {
		t.Errorf("content-type = %q, want image/webp", ct)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("missing nosniff header")
	}
	if rec.Header().Get("ETag") == "" || rec.Header().Get("Cache-Control") == "" {
		t.Errorf("missing ETag/Cache-Control")
	}
	if rec.Body.Len() == 0 {
		t.Errorf("empty body")
	}

	// If-None-Match with the served ETag yields 304.
	cond := httptest.NewRequest(http.MethodGet, path, nil)
	cond.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	condRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(condRec, cond)
	if condRec.Code != http.StatusNotModified {
		t.Errorf("conditional GET = %d, want 304", condRec.Code)
	}
}

func TestHandler_ServeUnknownHashIs404(t *testing.T) {
	h, _, _ := newHandler(t, Config{})
	path := "/" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" + ".png"
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_ServeMalformedHashIs404(t *testing.T) {
	h, _, _ := newHandler(t, Config{})
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/not-a-hash.png", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h, _, _ := newHandler(t, Config{})
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
