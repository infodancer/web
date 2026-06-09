package media

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// ingestCfg is a Config for the in-memory ingest tests: a valid category and a
// public base so URLs are exercised.
func ingestCfg() Config {
	return Config{Category: "cover", PublicBaseURL: "/media/cover"}
}

func TestIngest_StoresObjectAndReturnsURL(t *testing.T) {
	store := newFakeStore()
	blobs := NewMemoryBlobStore()
	cfg := ingestCfg()

	res, err := Ingest(context.Background(), store, blobs, cfg, "book-42", bytes.NewReader(pngBytes(t, 16, 16)))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.Existed {
		t.Errorf("Existed = true, want false on first ingest")
	}
	if res.Meta.Subject != "book-42" {
		t.Errorf("Meta.Subject = %q, want %q", res.Meta.Subject, "book-42")
	}

	// Bytes landed in the blob store under the content address.
	if ok, _ := blobs.Has(context.Background(), cfg.Category, res.Meta.Hash); !ok {
		t.Errorf("blob for hash %s not stored", res.Meta.Hash)
	}
	// Metadata row recorded with the subject.
	got, ok, _ := store.Lookup(context.Background(), cfg.Category, res.Meta.Hash)
	if !ok {
		t.Fatalf("object %s not recorded in store", res.Meta.Hash)
	}
	if got.Subject != "book-42" {
		t.Errorf("stored Subject = %q, want %q", got.Subject, "book-42")
	}

	// The returned URL must be byte-identical to what the serve path produces
	// for the same object — the anti-drift guarantee that Ingest exists for.
	h, err := NewHandler(store, blobs, cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if want := h.objectURL(res.Meta.Hash, res.Meta.ContentType); res.URL != want {
		t.Errorf("URL = %q, want serve-path URL %q", res.URL, want)
	}
	if want := "/media/cover/" + res.Meta.Hash + ".webp"; res.URL != want {
		t.Errorf("URL = %q, want %q (png re-encodes to webp)", res.URL, want)
	}
}

func TestIngest_TrailingSlashBase(t *testing.T) {
	store := newFakeStore()
	blobs := NewMemoryBlobStore()
	cfg := ingestCfg()
	cfg.PublicBaseURL = "/media/cover/" // trailing slash must not double up

	res, err := Ingest(context.Background(), store, blobs, cfg, "s", bytes.NewReader(pngBytes(t, 8, 8)))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if want := "/media/cover/" + res.Meta.Hash + ".webp"; res.URL != want {
		t.Errorf("URL = %q, want %q", res.URL, want)
	}
}

func TestIngest_IdempotentOnContent(t *testing.T) {
	store := newFakeStore()
	blobs := NewMemoryBlobStore()
	cfg := ingestCfg()
	src := pngBytes(t, 16, 16)

	first, err := Ingest(context.Background(), store, blobs, cfg, "s", bytes.NewReader(src))
	if err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	// Make any further store write fail. A correct idempotent path returns
	// early on Lookup and writes nothing, so the second ingest must still
	// succeed with Existed=true.
	store.putErr = errors.New("ingest must not write on re-ingest")

	second, err := Ingest(context.Background(), store, blobs, cfg, "s", bytes.NewReader(src))
	if err != nil {
		t.Fatalf("second Ingest: %v (idempotent path wrote to the store)", err)
	}
	if !second.Existed {
		t.Errorf("Existed = false, want true on re-ingest of identical bytes")
	}
	if second.URL != first.URL {
		t.Errorf("re-ingest URL = %q, want %q", second.URL, first.URL)
	}
	if second.Meta.Hash != first.Meta.Hash {
		t.Errorf("re-ingest hash = %q, want %q", second.Meta.Hash, first.Meta.Hash)
	}
}

func TestIngest_StoresThumbnails(t *testing.T) {
	store := newFakeStore()
	blobs := NewMemoryBlobStore()
	cfg := ingestCfg()
	cfg.Thumbnails = []ThumbnailSpec{{Name: "thumb", MaxWidth: 8, MaxHeight: 8}}

	res, err := Ingest(context.Background(), store, blobs, cfg, "s", bytes.NewReader(pngBytes(t, 32, 32)))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	key := cfg.Category + "\x00" + res.Meta.Hash + "\x00thumb"
	v, ok := store.variants[key]
	if !ok {
		t.Fatalf("variant %q not recorded", key)
	}
	if v.ParentHash != res.Meta.Hash {
		t.Errorf("variant ParentHash = %q, want %q", v.ParentHash, res.Meta.Hash)
	}
	if ok, _ := blobs.Has(context.Background(), cfg.Category, v.Hash); !ok {
		t.Errorf("thumbnail blob %s not stored", v.Hash)
	}
}

func TestIngest_InvalidConfig(t *testing.T) {
	store := newFakeStore()
	blobs := NewMemoryBlobStore()
	cfg := ingestCfg()
	cfg.Category = "" // invalid

	if _, err := Ingest(context.Background(), store, blobs, cfg, "s", bytes.NewReader(pngBytes(t, 8, 8))); err == nil {
		t.Error("Ingest with empty category = nil error, want error")
	}
}

func TestIngest_RejectsBadImage(t *testing.T) {
	store := newFakeStore()
	blobs := NewMemoryBlobStore()
	cfg := ingestCfg()

	_, err := Ingest(context.Background(), store, blobs, cfg, "s", bytes.NewReader([]byte("not an image")))
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("err = %v, want ErrUnsupportedFormat", err)
	}
}
