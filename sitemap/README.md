# sitemap

[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

Reusable Go module for XML sitemaps across infodancer/web sites. Used at two
altitudes in the same site: a **leaf** mount inside each content module
(`/faq/sitemap.xml`, `/blog/sitemap.xml`) serves that module's URLs as a
`<urlset>`, and an **index** mount at the host root (`/sitemap.xml`) stitches the
leaves into a `<sitemapindex>`.

**Status:** design. See [DESIGN.md](DESIGN.md). No code yet.

## Why

Content is live but unindexed because nothing in the stack produces a current
sitemap (the case that started this: speculativefiction.org's full book catalog
reported "URL is unknown to Google"). Each content module already knows how to
list its URLs -- the same enumeration that feeds search -- so the sitemap is
built once as a module and every tool contributes the same way.

## Design highlights

- **Explicit inclusion only.** No filesystem walk, no mux enumeration. A URL
  appears only because a registered `Source` emits it.
- **Leaf auto-promotes** to a paginated sub-index past the 50,000-URL /50 MB cap;
  the root index points at the leaf root either way.
- **Forced inclusion via the smoke manifest.** A `cmd/` gate (the `smolder gate`
  analog) fails CI when a public, read-only GET route carries no `sitemap` label
  -- include (a source name) or exclude (a reason). Built on a generic `Labels`
  field added to smoke, so smoke stays SEO-agnostic.
- **faq is the reference implementation** to extract -- it already serves a
  paginated sitemap index in production.

## Lineage

This module is unrelated to the dead 2019 Java `infodancer/sitemap` repo.
