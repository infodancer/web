package media

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a valid-looking content address (64 lowercase hex chars).
const testHash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"

func newFSStore(t *testing.T) (*FilesystemBlobStore, string) {
	t.Helper()
	root := t.TempDir()
	fs, err := NewFilesystemBlobStore(root)
	if err != nil {
		t.Fatalf("NewFilesystemBlobStore: %v", err)
	}
	return fs, root
}

func TestFilesystemBlobStore_RoundTrip(t *testing.T) {
	fs, _ := newFSStore(t)
	ctx := context.Background()
	payload := []byte("\x89PNG... canonical bytes")

	has, err := fs.Has(ctx, testHash)
	if err != nil || has {
		t.Fatalf("Has before Put = (%v, %v), want (false, nil)", has, err)
	}

	if err := fs.Put(ctx, testHash, "image/png", payload); err != nil {
		t.Fatalf("Put: %v", err)
	}

	has, err = fs.Has(ctx, testHash)
	if err != nil || !has {
		t.Fatalf("Has after Put = (%v, %v), want (true, nil)", has, err)
	}

	got, ct, err := fs.Get(ctx, testHash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("bytes round trip mismatch")
	}
	if ct != "image/png" {
		t.Errorf("content type = %q, want image/png", ct)
	}
}

func TestFilesystemBlobStore_ShardedLayout(t *testing.T) {
	fs, root := newFSStore(t)
	if err := fs.Put(context.Background(), testHash, "image/png", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	want := filepath.Join(root, testHash[:2], testHash[2:4], testHash)
	if _, err := os.Stat(want); err != nil {
		t.Errorf("blob not at sharded path %s: %v", want, err)
	}
}

func TestFilesystemBlobStore_MissingIsNotFound(t *testing.T) {
	fs, _ := newFSStore(t)
	_, _, err := fs.Get(context.Background(), testHash)
	if !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Get missing = %v, want ErrBlobNotFound", err)
	}
}

func TestFilesystemBlobStore_Idempotent(t *testing.T) {
	fs, _ := newFSStore(t)
	ctx := context.Background()
	if err := fs.Put(ctx, testHash, "image/png", []byte("first")); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	// Content-addressed: a second Put is a no-op and keeps the original bytes.
	if err := fs.Put(ctx, testHash, "image/png", []byte("second-ignored")); err != nil {
		t.Fatalf("Put 2: %v", err)
	}
	got, _, err := fs.Get(ctx, testHash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "first" {
		t.Errorf("idempotent Put overwrote bytes: got %q", got)
	}
}

// A key that is not a clean content address must be refused — no file may be
// created outside the sharded layout (path-traversal guard).
func TestFilesystemBlobStore_RejectsBadHash(t *testing.T) {
	fs, root := newFSStore(t)
	ctx := context.Background()
	for _, bad := range []string{
		"../../etc/passwd",
		"a/b",
		"AABBCC",                // too short / uppercase
		strings.Repeat("g", 64), // non-hex
	} {
		if err := fs.Put(ctx, bad, "image/png", []byte("x")); err == nil {
			t.Errorf("Put(%q) succeeded, want error", bad)
		}
		if _, _, err := fs.Get(ctx, bad); !errors.Is(err, ErrBlobNotFound) {
			t.Errorf("Get(%q) = %v, want ErrBlobNotFound", bad, err)
		}
		if has, _ := fs.Has(ctx, bad); has {
			t.Errorf("Has(%q) = true, want false", bad)
		}
	}
	// Nothing escaped the root.
	if entries, _ := os.ReadDir(filepath.Dir(root)); len(entries) > 0 {
		for _, e := range entries {
			if e.Name() == "passwd" {
				t.Fatalf("path traversal wrote outside the root")
			}
		}
	}
}

func TestFilesystemBlobStore_EmptyRootRejected(t *testing.T) {
	if _, err := NewFilesystemBlobStore(""); err == nil {
		t.Fatalf("empty root should error")
	}
}

// The filesystem backend drives the handler end to end, same as Postgres.
func TestFilesystemBlobStore_HandlerRoundTrip(t *testing.T) {
	fs, _ := newFSStore(t)
	h := NewHandler(newFakeStore(), fs, Config{PublicBaseURL: "/media"})

	up := httptest.NewRecorder()
	h.Routes().ServeHTTP(up, uploadRequest(t, "image", pngBytes(t, 14, 10)))
	if up.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201; body=%s", up.Code, up.Body.String())
	}
	var resp uploadResponse
	if err := json.Unmarshal(up.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	path := resp.URL[len("/media"):]
	get := httptest.NewRecorder()
	h.Routes().ServeHTTP(get, httptest.NewRequest(http.MethodGet, path, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("serve status = %d, want 200", get.Code)
	}
	if get.Header().Get("Content-Type") != "image/png" || get.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("serve headers wrong: ct=%q nosniff=%q",
			get.Header().Get("Content-Type"), get.Header().Get("X-Content-Type-Options"))
	}
}
