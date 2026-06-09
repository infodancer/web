# media-import

A small operator/scripting CLI for backfilling images into a
[`media`](../../) store. It is a thin wrapper over `media.Ingest`: validate a
source image, store the object, its blob, and any thumbnails, and print the
object's canonical public URL. Idempotent on content address, so re-running is
safe.

This is *not* an HTTP surface. Auth, CSRF, and the mapping from a source to the
owning record are the operator's concern. The in-process consumers (a site's
boot backfill) call `media.Ingest` directly; this CLI is the convenience layer
on the same code path, so the two cannot drift.

## Usage

```
media-import \
  --database-url $DATABASE_URL \
  --category cover \
  --subject <owning-record-id> \
  [--blob postgres|fs] [--blob-dir <dir>] \
  [--public-base-url /media/cover] \
  [--dry-run] [--json] \
  <file-or-dir-or-glob>... [--url <u>]...
```

Sources are files, directories (non-recursive; `*.{png,jpg,jpeg,gif,webp}`),
shell globs, and/or remote `--url` images (http/https, fetched with
`--fetch-timeout`).

### Output

One line per source:

```
<hash>  <url>  (new|exists)
```

or, with `--json`, one record per source so a consumer's repoint step can read
stdout:

```json
{"source":"covers/dune.jpg","hash":"<64-hex>","url":"/media/cover/<hash>.webp","width":400,"height":600,"existed":false}
```

In text mode, per-source failures are reported on stderr (so stdout stays
parseable); in `--json` mode they are `{"source":...,"error":...}` records on
stdout. A run exits non-zero if any source failed.

### `--dry-run`

Validates each source and reports its content address and dimensions without
writing. It needs no database. It does **not** print a URL: `media` mints the
canonical URL only when it stores the object, and that builder is intentionally
internal — take the URL from a real run's output.

## Backends

- `--blob postgres` (default): bytes in the `media_blobs` table alongside the
  object metadata. Matches the production deployments.
- `--blob fs --blob-dir <dir>`: sharded, atomic filesystem blobs under `<dir>`.

Object metadata always lives in Postgres (`--database-url`); the migrations run
automatically before ingest.

## Build

```
task build      # -> ./media-import
task check      # fmt, vet, lint, test, vulncheck
```
