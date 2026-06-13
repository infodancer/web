# infodancer/web/sitemap -- design (v0.1)

*Status: design. Initial design 2026-06-13. No code yet -- this document is
authoritative until code lands, after which the design + code together are.*

## 1. Purpose

`infodancer/web/sitemap` is a reusable Go module that produces XML sitemaps for
sites assembled from infodancer/web components plus their own custom code. It is
mounted in-process under a base path like every other component in the monorepo,
and it is deliberately used at **two altitudes** in the same site:

- a **leaf** mount inside each content module (e.g. `/faq/sitemap.xml`,
  `/blog/sitemap.xml`) that serves that module's own URLs as a `<urlset>`;
- an **index** mount at the host root (`/sitemap.xml`) that stitches the leaves
  together into a `<sitemapindex>`.

Same package, both jobs. A module that knows how to serve its URLs gains the
ability to list them; the host gains a one-line index over whatever leaves it
mounted.

### Why this module exists

The driving observation came from speculativefiction.org: the full book catalog
(hundreds of `/book/`, `/author/`, `/series/` pages) is live and public, yet
Search Console reports every catalog URL as *"URL is unknown to Google."* The
only registered sitemap is a dead 2022 entry from the domain's prior incarnation.
There is no current sitemap because nothing in the stack produces one. The same
gap exists for every site in the portfolio that mounts these components.

A sitemap could be bolted into each site by hand, but the portfolio already has
the same content-bearing modules (blog, faq, search, profile) mounted across
several sites, plus per-site custom surfaces (sf's book catalog, osg's
bestiary). Building the sitemap once as a module means each content module
contributes its URLs the same way everywhere, and custom site code plugs into
the identical mechanism.

### What the module is

- A Go module mountable under any base path, following the suite mount contract.
- A **leaf** serving one content source as a sitemap (`<urlset>`, auto-promoting
  to a paginated sub-index past the protocol cap).
- An **index** serving a `<sitemapindex>` over a host-supplied list of child
  leaf locations.
- A small, streamed `Source` interface that any module or custom function
  implements to contribute its URLs.
- A coverage **gate** (a `cmd/`, the `smolder gate` analog) that fails CI when a
  public, indexable route has no sitemap disposition -- forced inclusion, built
  on the existing smoke route manifest rather than a second route recorder.

### What the module is not

- Not a crawler or route-discovery tool. It never walks the filesystem or
  enumerates the mux to guess what is public. Nothing appears in a sitemap unless
  a source explicitly emits it. This is the central design constraint: inclusion
  is by hand, and the gate makes "by hand" safe rather than forgetful.
- Not an indexer or search store. It holds no persistent table of URLs; each
  source is the live authority for its own URLs (contrast with
  `infodancer/web/search`, which must persist a queryable index).
- Not the robots.txt manager. The `Sitemap:` directive is a deployment concern
  (on sf, robots.txt is Cloudflare-managed); the module only serves the XML.
- Not a ping/submission service. Notifying search engines is out of scope for
  v0.1 (see non-goals).

## 2. Non-goals (v0.1)

- **Search-as-a-source shortcut.** The search index already aggregates URLs from
  every module that feeds it, so a `searchMod.SitemapSource()` projection is
  tempting. Excluded by decision: "indexed for site search" and "belongs in
  Google's sitemap" are related but not identical sets (browse/genre hubs are
  pages but not search documents; some search documents should not be
  advertised). Sources are registered explicitly per site. A search projection
  can be reconsidered later as an opt-in source, never a default.
- **Search-engine ping/submission.** No automated sitemap submission. Google has
  deprecated the ping endpoint anyway; registration is a one-time GSC action.
- **News/image/video sitemap extensions.** Plain URL sitemaps only. Image
  entries (book covers) are a plausible v0.2 once the base path is solid.
- **`changefreq` / `priority`.** Google ignores both. The `Entry` type may carry
  them for completeness but the default output omits them.
- **Cross-site / multi-host sitemaps.** One host, one canonical origin per mount.

## 3. Architecture

### Module shape

Two constructors, two handlers, one source abstraction.

```go
package sitemap

// Entry is one URL in a sitemap.
type Entry struct {
    Loc     string    // root-relative path or absolute URL; joined to BaseURL
    LastMod time.Time // zero value omits <lastmod> (valid)
}

// Source streams a content area's URLs. The yield-iterator shape lets a
// 10k-row catalog stream straight to the response without materializing.
// Returning false from yield stops iteration (the writer hit its page cap).
type Source interface {
    SitemapEntries(ctx context.Context, yield func(Entry) bool) error
}

// SourceFunc adapts a plain function -- the door for custom site code
// (sf's catalog, osg's bestiary). Mirrors the suite's UserResolverFunc.
type SourceFunc func(ctx context.Context, yield func(Entry) bool) error
func (f SourceFunc) SitemapEntries(ctx context.Context, yield func(Entry) bool) error { return f(ctx, yield) }
```

**Leaf** -- one source, served as a sitemap:

```go
type LeafConfig struct {
    BaseURL    string        // canonical origin, e.g. https://speculativefiction.org
    Source     Source
    CacheTTL   time.Duration // 0 = regenerate per request; each module chooses
    MaxPerPage int           // default 50000 (protocol cap)
}
func NewLeaf(LeafConfig) (*Leaf, error)
func (l *Leaf) Routes() http.Handler
```

The leaf serves a `<urlset>` when the source fits in one page, and
auto-promotes to a `<sitemapindex>` referencing `?p=1..N` (paginated children
under its own mount) once it overflows `MaxPerPage` or the 50 MB byte cap. The
index above it does not care which form a leaf takes -- it points at the leaf
root either way. A three-entry faq and a 40k-book catalog use identical wiring.

**Index** -- the host's stitch over child leaves:

```go
type ChildRef struct {
    Loc     string    // location of a child sitemap, e.g. "/faq/sitemap.xml"
    LastMod time.Time // optional; omitted if zero
}
type IndexConfig struct {
    BaseURL  string
    Children []ChildRef
    CacheTTL time.Duration // lazy TTL is right for the root index
}
func NewIndex(IndexConfig) (*Index, error)
func (i *Index) Routes() http.Handler
```

### The two mounts in practice

Each content module mounts its own leaf inside its router, so sitemap handling
lives in the tool that owns the content:

```go
// inside faq's Routes(), reusing faq's own content enumeration
leaf, _ := sitemap.NewLeaf(sitemap.LeafConfig{
    BaseURL: cfg.BaseURL,
    Source:  m.SitemapSource(), // thin adapter over faq's existing list query
})
mux.Handle("GET /sitemap.xml", leaf.Routes()) // -> /faq/sitemap.xml once host-mounted
```

The host mounts the index and any custom leaves:

```go
// sf's catalog leaf reuses the ListBooksForIndex/Authors/Series queries
// that already feed search -- a SourceFunc over the same enumeration.
catalog, _ := sitemap.NewLeaf(sitemap.LeafConfig{
    BaseURL: cfg.BaseURL,
    Source:  sitemap.SourceFunc(s.catalogSitemapEntries),
})
s.mux.Handle("GET /sitemap-catalog.xml", catalog.Routes())

idx, _ := sitemap.NewIndex(sitemap.IndexConfig{
    BaseURL:  cfg.BaseURL,
    Children: []sitemap.ChildRef{
        {Loc: "/sitemap-catalog.xml"},
        {Loc: "/blog/sitemap.xml"},
        {Loc: "/faq/sitemap.xml"},
    },
    CacheTTL: 15 * time.Minute,
})
s.mux.Handle("GET /sitemap.xml", idx.Routes())
```

The index child list is supplied by hand -- the host knows its mount paths. The
gate (below) ensures nothing public is silently missing from that list.

### Caching

- The **root index** uses a lazy TTL: regenerate on the first request after
  expiry, serve the cached document otherwise. The index is cheap (a handful of
  child refs) but the TTL bounds work under crawler bursts.
- Each **leaf** chooses its own policy via `CacheTTL`. A small faq leaf can
  regenerate every request; a large catalog leaf sets a TTL or relies on its own
  module-level cache. Modules make their own choices -- the module does not
  impose one cache strategy on every content area.

### Paths and content type

- Root index at `/sitemap.xml` (crawlers probe that literal path).
- Leaves at `/<base>/sitemap.xml`. The `.xml` extension is cosmetic --
  `Content-Type: application/xml` drives parsing -- but it reads clearly and
  matches crawler expectations.

## 4. Forced inclusion -- riding the smoke manifest

The requirement: adding a new public page must not silently leave it out of the
sitemap. The model is smoke/smolder, where the mux wrapper records a `RouteSpec`
for every registered route and `smolder gate` fails CI on any route lacking
coverage. The same recorder already runs in these hosts, so the sitemap gate
**reuses the smoke manifest** rather than introducing a second route recorder.

### Mechanism

1. **Generic labels on smoke.** `smoke.RouteSpec` gains a generic
   `Labels map[string]string` field (with a `smoke.Label(key, value)` option)
   that smoke records and serializes but never interprets. This keeps smoke a
   pure route-metadata registry; sitemap is one consumer of a label namespace,
   and the next tool that wants to ride the manifest claims its own namespace
   instead of bloating smoke's API. (This is the one change required outside this
   module; it is small and additive.)

2. **Sitemap claims the `sitemap` label.** Each route declares its disposition
   at registration:

   ```go
   mux.HandleFunc("GET /book/{slug}", show,
       smoke.Example("slug", "23-years-on-fire"),
       smoke.Label("sitemap", "catalog"))            // covered by the catalog source

   mux.HandleFunc("GET /login", login,
       smoke.Label("sitemap", "exclude:auth page"))  // consciously excluded
   ```

3. **The gate.** A new `cmd/` in this module (the `smolder gate` analog) reads a
   smoke manifest file and fails when any route that is **GET (or unspecified),
   read-only, not auth-required, and not skipped** carries no `sitemap` label.
   Add a public page, forget the sitemap, and CI breaks until the route is given
   an `include` (a source name) or `exclude:<reason>`.

### What the gate does and does not verify

The gate works at the **pattern** level: `sitemap=catalog` on `/book/{slug}`
asserts "the catalog source is responsible for instances of this pattern." It
does not, offline, confirm the source actually emits those concrete URLs -- the
same offline-gate philosophy smolder already uses. A deeper live check (probe
the served sitemap, diff its URLs against the smoke-probed routes) is a v0.2
candidate. v0.1 forces the conscious decision per route, which is the property
that was missing.

### Dependency direction

sitemap depends on smoke's manifest **format** (it reads the JSON); smoke never
imports sitemap. The label field is generic, so smoke stays SEO-agnostic.

## 5. faq is the reference implementation to extract

This module is not greenfield. **faq already ships the entire pattern** in
`faq/internal/handler/handler_sitemap.go`: a `GET /sitemap.xml` that emits a
`<sitemapindex>`, `GET /sitemap/{name}` parts paginated at the 50,000-URL
sitemaps.org cap, the `<urlset>`/`<sitemapindex>` XML structs and namespace, an
`absoluteBase` helper, and even subsite-scoped sub-sitemaps. The leaf's
"auto-promote a single source to a paginated sub-index" behavior described in
section 3 is precisely what faq's index-plus-parts already does in production.

So the build order is **extract, then adopt**:

1. Lift faq's proven sitemap logic into this module as the generic `Leaf`
   (urlset + auto-paginated sub-index) and `Index` (sitemapindex over child
   refs), parameterized by `Source` and `BaseURL` instead of faq's store.
2. Refactor faq to mount the shared `Leaf` over a `faq.SitemapSource()` adapter
   and delete its bespoke `handler_sitemap.go`. **Deferred** -- see the finding
   below.
3. New consumers (sf catalog, blog, osg bestiary) mount leaves over their own
   sources.

### Finding: faq's subsite model does not adopt cleanly (v0.1)

Implementation surfaced a real mismatch. faq's sitemap is not a single source
served as one leaf: it is a per-request **index over a dynamic set of subsites**
(`ListSubsites` queried at request time), served through one `GET /sitemap/{name}`
route that dispatches by parsing the name (`{N}` for the global corpus,
`subsite-{slug}-{N}` for a subsite). The shared module's leaf has a fixed `Name`
and fixed routes, and the shared index takes a static `Children` list at
construction. Neither represents a runtime-varying family of sub-sitemaps.

A faithful adoption needs two generalizations the v0.1 module deliberately does
not have: a **dynamic children provider** on `Index` (a `func(ctx) ([]ChildRef,
error)`), and a **parameterized leaf family** (one logical leaf addressing many
runtime keys through a single dispatched route). Both are reasonable additions,
but they are a design step, not a mechanical refactor, and adding them now would
either bloat the module for one consumer's edge case or leave faq half-migrated.

Crucially, **nothing downstream needs the faq refactor**: a host's root index
just references faq's existing `/faq/sitemap.xml`, which faq already serves
correctly. So faq adoption is decoupled from every other consumer and is held
for a v0.2 design pass (tracked in infodancer/web#70). The module's `Leaf`/
`Index` are still extractions of faq's proven XML output and pagination -- the
global-corpus case maps directly; only the dynamic subsite layer is deferred.

Every module that already feeds search has the enumeration a source needs --
`blog.ReindexAll`, sf's `ListBooksForIndex` / `ListAuthorsForIndex` /
`ListSeriesForIndex`. A `SitemapSource()` on each module is a thin adapter over
that existing enumeration, yielding `Entry{Loc, LastMod}` from rows the module
already lists. `lastmod` comes from each module's own updated-at columns; sources
without timestamps omit it (valid). blog's DESIGN already lists "a sitemap" among
its SEO output -- this module is where that leaf comes from, rather than blog
growing its own XML writer.

## 6. Open questions / deferred

- **Image entries for covers.** sf book pages have cover image URLs (already in
  their JSON-LD). An image-sitemap extension on the catalog leaf is a strong
  v0.2 once the base sitemap is indexed.
- **Live coverage check.** The v0.2 gate that diffs served sitemap URLs against
  smoke-probed routes (see section 4).
- **Optional search projection.** Re-evaluate exposing the search index as an
  opt-in source for sites that genuinely want "everything indexed -> sitemap."
- **faq subsite adoption.** The dynamic children + parameterized leaf
  generalization from the section 5 finding (infodancer/web#70).

## 7. First consumer

speculativefiction.org: a catalog leaf (`/sitemap-catalog.xml`) over the
existing index queries, a blog leaf, faq's existing `/faq/sitemap.xml` referenced
as-is (faq is not yet refactored onto the module -- see section 5), and a root
index at `/sitemap.xml` listing all three. Tracked in speculativefiction/sf#39.
Cleaning up the stale 2022 GSC sitemap and adding the robots.txt `Sitemap:`
directive are deployment steps in that issue, outside this module.
