# 0004: Add optional `headline` field for compact lane cards

**Status:** open
**Area:** schema / rendering
**Priority:** low

## Problem

The `title` field serves two display roles that pull in opposite directions:

1. **Lane card** in the timeline view, where space is tight and a long title wraps awkwardly or gets truncated.
2. **Detail page** `<h1>`, OG `og:title`, search results, and feeds, where a fully descriptive title aids comprehension and search ranking.

Authors are forced to compromise — either write a short title that's underdescriptive on the detail page, or a long title that breaks the lane view.

## Proposal

Add an optional `headline?: string` field for the compact representation.

**Schema:**
```cue
#Event: {
    title:     string
    headline?: string  // short form for compact card views; falls back to title
    // ...
}
```

**Rendering rule:** any compact-card view (lane card, search-result row, related-event card) uses `headline` if present, else `title`. Detail page `<h1>`, OG tags, feed entries, and breadcrumbs always use `title`.

**Suggested length budget for authors:** ≤ 10 words. Not enforced by schema; tooling on the data-repo side can lint.

## Acceptance criteria

- New optional `headline` field accepted by schema, sync, and API.
- Lane cards and search-result rows render `headline` when present, `title` otherwise.
- Detail page `<h1>`, OG tags, and feeds always render `title`.
- Existing events with no `headline` continue to render unchanged.
