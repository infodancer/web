package media

import (
	"context"
	"fmt"
	"io"
	"time"
)

// IngestResult reports the outcome of an [Ingest] call.
type IngestResult struct {
	// Meta is the stored object metadata: Category, Hash, Subject, dimensions,
	// ContentType, ByteSize, and CreatedAt.
	Meta ObjectMeta
	// URL is the object's canonical public URL, built by media from
	// Config.PublicBaseURL. Consumers store this verbatim and never recompute
	// it; the URL scheme is media's private contract.
	URL string
	// Existed is true when the content was already stored under the category, in
	// which case Ingest wrote nothing and returned the existing object's URL.
	Existed bool
}

// Ingest validates and stores one source image under cfg.Category for the given
// subject, writing the object, its blob, and any cfg.Thumbnails, and returns the
// object's canonical public URL. It is the reusable half of the
// Process -> store -> URL sequence that consumers would otherwise hand-roll (and
// in which they would have to mirror media's unexported URL builder).
//
// Ingest is idempotent on content address: a re-ingest of identical bytes is a
// no-op that returns Existed=true with the already-stored object's URL. The
// store argument is satisfied by [*Store]; tests use an in-memory fake.
//
// Auth, CSRF, category selection, and the subject mapping are the caller's
// concern — Ingest is an in-process operator/boot path, not an HTTP surface.
// The subject is an opaque owning-record id, never interpreted by media.
func Ingest(ctx context.Context, store objectStore, blobs BlobStore, cfg Config, subject string, r io.Reader) (IngestResult, error) {
	if err := validateConfig(cfg); err != nil {
		return IngestResult{}, fmt.Errorf("media: Ingest: %w", err)
	}

	processed, err := Process(r, cfg)
	if err != nil {
		return IngestResult{}, fmt.Errorf("media: Ingest: %w", err)
	}

	// Idempotency: if this content address is already recorded under the
	// category, return its URL without rewriting any bytes.
	if existing, ok, err := store.Lookup(ctx, cfg.Category, processed.SHA256); err != nil {
		return IngestResult{}, fmt.Errorf("media: Ingest: lookup: %w", err)
	} else if ok {
		return IngestResult{
			Meta:    existing,
			URL:     objectURL(cfg.PublicBaseURL, existing.Hash, existing.ContentType),
			Existed: true,
		}, nil
	}

	// Blob before metadata: a crash between the two leaves bytes with no row,
	// which a re-run heals (blob Put is a no-op, then the row is written). The
	// reverse order would let a Lookup report Existed for an object whose bytes
	// were never stored.
	if err := blobs.Put(ctx, cfg.Category, processed.SHA256, processed.ContentType, processed.Bytes); err != nil {
		return IngestResult{}, fmt.Errorf("media: Ingest: store blob: %w", err)
	}
	meta := ObjectMeta{
		Category:    cfg.Category,
		Hash:        processed.SHA256,
		ContentType: processed.ContentType,
		Width:       processed.Width,
		Height:      processed.Height,
		ByteSize:    int64(len(processed.Bytes)),
		Subject:     subject,
		CreatedAt:   time.Now().UTC(),
	}
	if err := store.Put(ctx, meta); err != nil {
		return IngestResult{}, fmt.Errorf("media: Ingest: store object: %w", err)
	}

	// Each pregenerated thumbnail is stored like any other content-addressed
	// blob (served by the existing GET path) with a media_variants row mapping
	// the original to its named size.
	now := time.Now().UTC()
	for _, t := range processed.Thumbnails {
		if err := blobs.Put(ctx, cfg.Category, t.SHA256, t.ContentType, t.Bytes); err != nil {
			return IngestResult{}, fmt.Errorf("media: Ingest: store thumbnail %q blob: %w", t.Name, err)
		}
		variant := Variant{
			Category:    cfg.Category,
			ParentHash:  processed.SHA256,
			Name:        t.Name,
			Hash:        t.SHA256,
			ContentType: t.ContentType,
			Width:       t.Width,
			Height:      t.Height,
			ByteSize:    int64(len(t.Bytes)),
			CreatedAt:   now,
		}
		if err := store.PutVariant(ctx, variant); err != nil {
			return IngestResult{}, fmt.Errorf("media: Ingest: store thumbnail %q variant: %w", t.Name, err)
		}
	}

	return IngestResult{
		Meta:    meta,
		URL:     objectURL(cfg.PublicBaseURL, meta.Hash, meta.ContentType),
		Existed: false,
	}, nil
}
