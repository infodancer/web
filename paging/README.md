# paging

Offset-based pagination primitives shared across the `infodancer/web`
components and the sites that host them. This is the **data-layer** half of
pagination; the **presentation** half is `infodancer/ui`'s `Pager` /
`ui/pager` partial. Together:

```
paging.Paginate ──► paging.Page ──► ui.NewPager ──► {{template "ui/pager"}}
   (is there a            (one page          (prev/next URLs)     (the nav)
    next page?)            of rows)
```

## Why it exists

Every list page needs the same three things: clamp the requested offset,
fetch one page, and know whether a *next* page exists -- without paying for a
`COUNT(*)`. Each component was re-deriving that (or, worse, silently
truncating at a hard `LIMIT`). This package owns the over-fetch math once so
the components don't.

## What it is not

- Not a query builder or an ORM. `Paginate` takes a **fetch closure**; the
  host writes its own typed query (pgx/sqlc, `database/sql`, or an in-memory
  slice). The package imports no database driver.
- Not cursor/keyset paging. Offset/limit is enough at these page sizes;
  keyset is a scale problem to solve when a table reaches it.
- Not numbered "page X of Y". That needs a total count; this deliberately
  avoids the second query. A consumer that wants it can keep its own count.

## Usage

```go
// 1. Parse + clamp the request (or build paging.Params directly for a
//    fixed, non-user-selectable page size).
pp := paging.ParseParams(r, 50, 200) // default 50, max 200

// 2. Fetch one page. Paginate over-fetches by one row to detect HasNext.
page, err := paging.Paginate(pp, func(limit, offset int) ([]*Campaign, error) {
    return store.ListCampaigns(ctx, limit, offset)
})
if err != nil { /* ... */ }

// 3. Hand the result to the presentation pager.
pager := ui.NewPager("/campaign/", r.URL.Query(),
    page.Offset, page.PageSize, len(page.Items), page.HasNext)
```

For a page whose size is fixed (not reader-selectable), skip `ParseParams`
and build `paging.Params{Offset: parsedOffset, PageSize: pageSizeConst}`
yourself, so no `?size=` can change it.

## API

| Symbol | Purpose |
|---|---|
| `Params{Offset, PageSize}` | one page request |
| `Page[T]{Items, Offset, PageSize, HasPrev, HasNext}` | one page of results + the prev/next signal |
| `Paginate[T](Params, fetch) (Page[T], error)` | over-fetch by one, trim, set `HasNext` |
| `ParseParams(r, defaultSize, maxSize) Params` | read+clamp `?offset=`/`?size=` |

`Paginate` clamps a `PageSize < 1` to 1 and a negative `Offset` to 0, so a
malformed request can never ask the fetch closure for a non-positive limit.

See `SECURITY.md` for the trust boundary (the `fetch` closure and its SQL are
the host's responsibility, not this package's).
