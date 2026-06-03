# 0003: Add `analysis` field, displayed only on the full event page

**Status:** open
**Area:** schema / rendering
**Priority:** medium

## Problem

The current `article` field is overloaded: it carries both factual narrative (the event as it occurred, sourced) and authorial analysis or commentary (interpretation, framing, predictions). Authors signal the distinction informally (e.g., wrapping commentary in italics), but the renderer treats both the same — they share a column, scroll together, and are weighted equally for SEO and metadata.

This forces a tradeoff: keep `article` short and lose the analytical register, or include analysis and let the page bloat.

## Proposal

Add a new optional event field `analysis: string` (Markdown), separate from `article`.

**Schema (`internal/web/schemas/timeline.cue`):**
```cue
#Event: {
    // ...existing fields...
    article?:  string  // factual narrative, sourced
    analysis?: string  // editorial / interpretive content
}
```

**Storage:** new column `events.analysis text` via migration; sqlc regen; CUE sync handles it.

**Rendering:**
- Inline expansion (`event_inline.html`): never shows `analysis`.
- Detail page (`event_detail.html`): renders `analysis` in a clearly distinct block — e.g., a labeled section below the article body titled "Analysis" with a left border accent, or a sidebar aside on wider viewports. Visually unmistakable as authorial commentary, not factual reporting.
- Meta tags (`og:description`, `<meta description>`): use `summary` only. Never `analysis`.

## Why a separate field, not a Markdown convention

A Markdown convention (e.g., "wrap analysis in `> ` blockquotes") couples authoring to one specific render and makes it impossible to suppress analysis in inline views, RSS, or programmatic consumers without parsing Markdown. A typed field is portable: API consumers can request it or not, the renderer can style it independently, and length budgets can be enforced per field.

## Acceptance criteria

- New optional `analysis` field accepted by the CUE schema, sync tool, and API.
- `analysis` content appears on the detail page in a visually distinct section.
- `analysis` content does NOT appear in the inline expansion, OG tags, or feed summaries.
- Removing `analysis` from an event removes the section without breaking layout.
