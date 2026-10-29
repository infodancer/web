// Package media is a reusable image upload/store/serve component for infodancer
// websites: a host mounts it, an author uploads an image, and the component
// makes it safe, stores it, and serves it under a stable content-addressed URL.
//
// Serving user-uploaded files is a stored-XSS and privacy vector. media
// concentrates that risk in one audited place — the way infodancer/ui/markdown
// concentrates Markdown sanitization. Every upload is sniffed by content (never
// by extension or the client's declared type), checked against a PNG/JPEG/GIF/
// WebP allowlist, guarded against decompression bombs before decode, and
// re-encoded to a canonical raster that strips EXIF and neutralizes polyglots
// (lossless WebP for PNG/GIF/WebP, q85 JPEG for JPEG; WebP encode is pure Go);
// SVG, animated GIF, and animated WebP are rejected. The result is
// content-addressed by SHA-256
// and served with Content-Type, X-Content-Type-Options: nosniff, immutable
// caching, and ETag.
//
// A handler configured with [Config.Thumbnails] also pregenerates derived sizes
// for every upload: each [ThumbnailSpec] yields a downscaled variant through the
// same pipeline (re-encoded, EXIF stripped), content-addressed by its own bytes
// and stored in the same blob store under the same category — so the GET path
// serves it unchanged. [BackfillThumbnails] fills variants for images stored
// before a spec was added.
//
// Images are partitioned by category — a server-side bucket like "campaign",
// "creature", or "character", fixed per handler and never set by the client.
// One configured store serves every category; a host registers one handler per
// category, giving physical separation (<root>/<category>/ab/cd/<hash> on disk,
// a composite key in the database) alongside the content address. An upload is
// always tied to some owning record the user may write to (e.g. a campaign);
// the host's authorize hook supplies that record's id (the subject) and the
// permission check.
//
// The host integration surface follows the suite mount contract:
//
//   - [Process] is the pure validation/transform pipeline (no database).
//   - [NewStore] wraps a pgx pool for object metadata; [NewPostgresBlobStore]
//     stores the bytes (default backend) behind the [BlobStore] seam, with
//     [NewFilesystemBlobStore] as a disk alternative.
//   - [NewHandler] returns the upload (POST) + serve (GET) routes to mount; its
//     [Config] fixes the category and the authorize hook.
//   - [Ingest] stores an image from a trusted in-process caller; [Delete]
//     removes an object, its variants, and any blob no longer in use.
//   - [Migrate] installs the schema under a dedicated goose version table so it
//     coexists with the host's own schema.
//
// Authentication, authorization, CSRF, and rate-limit policy belong to the
// host, exposed as hooks on [Config]. The category and the owning record
// (subject) are opaque host-defined strings, never foreign keys into a host
// table.
//
// See DESIGN.md for architecture and rationale.
package media
