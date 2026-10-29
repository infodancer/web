package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"
)

// hashOf returns the content address media would assign to data.
func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// testBlobStoreDelete is the Delete contract every BlobStore backend must meet.
// Each backend's test calls it with a constructor for an empty store.
func testBlobStoreDelete(t *testing.T, newStore func(t *testing.T) BlobStore) {
	t.Helper()
	ctx := context.Background()

	t.Run("removes the blob", func(t *testing.T) {
		bs := newStore(t)
		data := []byte("delete me")
		h := hashOf(data)
		if err := bs.Put(ctx, "cover", h, "image/png", data); err != nil {
			t.Fatalf("Put: %v", err)
		}
		if err := bs.Delete(ctx, "cover", h); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if ok, err := bs.Has(ctx, "cover", h); err != nil || ok {
			t.Errorf("Has after Delete = %v, %v; want false, nil", ok, err)
		}
		if _, _, err := bs.Get(ctx, "cover", h); !errors.Is(err, ErrBlobNotFound) {
			t.Errorf("Get after Delete err = %v, want ErrBlobNotFound", err)
		}
	})

	t.Run("missing blob is not an error", func(t *testing.T) {
		bs := newStore(t)
		if err := bs.Delete(ctx, "cover", hashOf([]byte("never stored"))); err != nil {
			t.Errorf("Delete of missing blob: %v, want nil", err)
		}
	})

	t.Run("is scoped to the category", func(t *testing.T) {
		bs := newStore(t)
		data := []byte("shared bytes")
		h := hashOf(data)
		for _, cat := range []string{"cover", "avatar"} {
			if err := bs.Put(ctx, cat, h, "image/png", data); err != nil {
				t.Fatalf("Put %s: %v", cat, err)
			}
		}
		if err := bs.Delete(ctx, "cover", h); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if ok, _ := bs.Has(ctx, "avatar", h); !ok {
			t.Error("Delete in one category removed the blob from another")
		}
	})

	t.Run("blob can be stored again after delete", func(t *testing.T) {
		bs := newStore(t)
		data := []byte("round trip")
		h := hashOf(data)
		for i := 0; i < 2; i++ {
			if err := bs.Put(ctx, "cover", h, "image/png", data); err != nil {
				t.Fatalf("Put #%d: %v", i, err)
			}
			if i == 0 {
				if err := bs.Delete(ctx, "cover", h); err != nil {
					t.Fatalf("Delete: %v", err)
				}
			}
		}
		got, ct, err := bs.Get(ctx, "cover", h)
		if err != nil || string(got) != string(data) || ct != "image/png" {
			t.Errorf("Get after re-Put = %q, %q, %v", got, ct, err)
		}
	})
}

func TestMemoryBlobStore_Delete(t *testing.T) {
	testBlobStoreDelete(t, func(*testing.T) BlobStore { return NewMemoryBlobStore() })
}

func TestFilesystemBlobStore_Delete(t *testing.T) {
	testBlobStoreDelete(t, func(t *testing.T) BlobStore {
		fs, err := NewFilesystemBlobStore(t.TempDir())
		if err != nil {
			t.Fatalf("NewFilesystemBlobStore: %v", err)
		}
		return fs
	})
}

func TestFilesystemBlobStore_DeleteRemovesSidecar(t *testing.T) {
	fs, err := NewFilesystemBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemBlobStore: %v", err)
	}
	ctx := context.Background()
	data := []byte("with sidecar")
	h := hashOf(data)
	if err := fs.Put(ctx, "cover", h, "image/png", data); err != nil {
		t.Fatalf("Put: %v", err)
	}
	blobPath, typePath, _ := fs.paths("cover", h)
	if err := fs.Delete(ctx, "cover", h); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	for _, p := range []string{blobPath, typePath} {
		if fileExists(p) {
			t.Errorf("%s still exists after Delete", p)
		}
	}
}

func TestFilesystemBlobStore_DeleteRejectsBadKeys(t *testing.T) {
	fs, err := NewFilesystemBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemBlobStore: %v", err)
	}
	ctx := context.Background()
	good := hashOf([]byte("x"))
	for _, tc := range []struct{ category, hash string }{
		{"../etc", good},
		{"Cover", good},
		{"cover", "../../../etc/passwd"},
		{"cover", "nothex"},
	} {
		if err := fs.Delete(ctx, tc.category, tc.hash); err == nil {
			t.Errorf("Delete(%q, %q) = nil, want an error", tc.category, tc.hash)
		}
	}
}
