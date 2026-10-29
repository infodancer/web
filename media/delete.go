package media

import (
	"context"
	"fmt"
)

// objectDeleter is the metadata side of [Delete]. [*Store] satisfies it.
type objectDeleter interface {
	DeleteObject(ctx context.Context, category, hash string) (unreferenced []string, err error)
}

// Delete removes the object (category, hash): its metadata, its variants, and
// every blob of theirs that nothing else in the category still refers to.
// Content addressing lets one blob back several objects or variants, so a blob
// still in use elsewhere is kept. Deleting an object that does not exist is not
// an error, and any blob left under its hash is removed.
//
// Metadata goes first, then bytes. A failure between the two leaves unused
// bytes behind, never a row pointing at missing bytes, and calling Delete
// again finishes the job.
//
// Delete must not race an [Ingest] or upload of the same image in the same
// category: the upload can find the bytes present, skip writing them, and then
// lose them to the delete. Callers that delete in bulk (for example, a sweep
// of objects their records no longer reference) should run it serialized with
// ingestion. A later re-ingest of the same image restores the bytes.
func Delete(ctx context.Context, store objectDeleter, blobs BlobStore, category, hash string) error {
	if !ValidCategory(category) || !looksLikeHash(hash) {
		return fmt.Errorf("media: Delete: invalid category/hash %q/%q", category, hash)
	}
	unreferenced, err := store.DeleteObject(ctx, category, hash)
	if err != nil {
		return fmt.Errorf("media: Delete: %w", err)
	}
	for _, h := range unreferenced {
		if err := blobs.Delete(ctx, category, h); err != nil {
			return fmt.Errorf("media: Delete: blob %s: %w", h, err)
		}
	}
	return nil
}

// Compile-time proof the concrete store satisfies Delete's interface.
var _ objectDeleter = (*Store)(nil)
