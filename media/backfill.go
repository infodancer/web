package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
)

// backfillPageSize is how many objects BackfillThumbnails walks per query.
const backfillPageSize = 100

// BackfillResult reports what a [BackfillThumbnails] run did.
type BackfillResult struct {
	Scanned int // originals examined
	Created int // variant rows newly written (missing sizes filled)
}

// BackfillThumbnails generates any missing thumbnails for images already stored
// in cfg.Category, using cfg.Thumbnails as the desired set. For each original it
// skips sizes that already exist, decodes the stored canonical bytes, generates
// only the missing sizes, and stores them (bytes to the BlobStore under their
// own content address, a media_variants row for the map). It is idempotent and
// safe to re-run — a completed object is a no-op on the next pass.
//
// Use it after adding the feature to an existing corpus, or after adding a new
// size to a handler's Config.Thumbnails. New uploads do not need it (they
// pregenerate at upload time). An empty cfg.Thumbnails is a no-op.
func BackfillThumbnails(ctx context.Context, store *Store, blobs BlobStore, cfg Config) (BackfillResult, error) {
	var res BackfillResult
	if cfg.Category == "" || !ValidCategory(cfg.Category) {
		return res, fmt.Errorf("media: backfill: invalid category %q", cfg.Category)
	}
	if err := validateThumbnailSpecs(cfg.Thumbnails); err != nil {
		return res, fmt.Errorf("media: backfill: %w", err)
	}
	if len(cfg.Thumbnails) == 0 {
		return res, nil
	}

	cursor := ""
	for {
		objs, err := store.ListObjectsAfter(ctx, cfg.Category, cursor, backfillPageSize)
		if err != nil {
			return res, fmt.Errorf("media: backfill: %w", err)
		}
		if len(objs) == 0 {
			break
		}
		for _, obj := range objs {
			res.Scanned++
			created, err := backfillOne(ctx, store, blobs, cfg, obj)
			if err != nil {
				return res, err
			}
			res.Created += created
		}
		cursor = objs[len(objs)-1].Hash
		if len(objs) < backfillPageSize {
			break
		}
	}
	return res, nil
}

// backfillOne fills the missing thumbnail sizes for a single original and
// returns how many it created.
func backfillOne(ctx context.Context, store *Store, blobs BlobStore, cfg Config, obj ObjectMeta) (int, error) {
	existing, err := store.ListVariants(ctx, obj.Category, obj.Hash)
	if err != nil {
		return 0, fmt.Errorf("media: backfill: %w", err)
	}
	have := make(map[string]struct{}, len(existing))
	for _, v := range existing {
		have[v.Name] = struct{}{}
	}
	var missing []ThumbnailSpec
	for _, spec := range cfg.Thumbnails {
		if _, ok := have[spec.Name]; !ok {
			missing = append(missing, spec)
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}

	// Regenerate from the stored canonical bytes (already safe and re-encoded).
	data, _, err := blobs.Get(ctx, obj.Category, obj.Hash)
	if err != nil {
		return 0, fmt.Errorf("media: backfill: get %s/%s: %w", obj.Category, obj.Hash, err)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("media: backfill: decode %s/%s: %w", obj.Category, obj.Hash, err)
	}
	thumbs, err := generateThumbnails(img, format, missing)
	if err != nil {
		return 0, fmt.Errorf("media: backfill: %w", err)
	}

	created := 0
	for _, t := range thumbs {
		if err := blobs.Put(ctx, obj.Category, t.SHA256, t.ContentType, t.Bytes); err != nil {
			return created, fmt.Errorf("media: backfill: put blob: %w", err)
		}
		v := Variant{
			Category:    obj.Category,
			ParentHash:  obj.Hash,
			Name:        t.Name,
			Hash:        t.SHA256,
			ContentType: t.ContentType,
			Width:       t.Width,
			Height:      t.Height,
			ByteSize:    int64(len(t.Bytes)),
			CreatedAt:   obj.CreatedAt,
		}
		if err := store.PutVariant(ctx, v); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}
