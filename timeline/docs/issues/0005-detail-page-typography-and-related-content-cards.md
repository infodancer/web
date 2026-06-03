# 0005: Detail-page typography and related-content card styling

**Status:** open
**Area:** styling / UX
**Priority:** medium

## Problem

The event detail page (`event_detail.html`) is structurally fine — main column with article body, aside with people and related events — but its visual treatment is generic. For events with substantial article body, this produces a long, undifferentiated reading experience that doesn't scale.

Specific problems:
- Content column is full-width on most viewports; long lines hurt readability.
- No distinct typographic hierarchy for H2/H3 within the article (now possible after #0001).
- Sources render as a numbered text list. With 10–20 sources per event this becomes hard to scan.
- Related events render as a stacked list of `<a>` + relationship text + free-form description. Hard to scan at a glance; no visual handle on date or category.

## Proposal

Three independent improvements; can ship as one PR or three.

**A. Article column typography**

- Constrain article column to `max-width: 70ch` (~640px) on wide viewports.
- Increase line-height to 1.6–1.7 for body copy.
- Style H2/H3 inside `.event-body` with clear hierarchy (size, weight, top-margin).
- Style `blockquote` with a left border and slight indent.
- After #0003, style `.event-analysis` as a distinct block (left accent border or boxed background, labeled heading).

**B. Sources as cards**

- Replace `<ol class="source-list">` with a grid or stacked card list.
- Each card: site favicon (fetch from `/favicon.ico` of the source domain at sync time, cache locally) + source title + author/citation as secondary text.
- Cards link the whole tile to the URL.

**C. Related events as cards**

- Replace the related-events stacked list with a card-style block per related event.
- Each card: date + category chip + headline (or title) + relationship label.
- Description text de-emphasized or hidden behind a hover/expand if present.

## Acceptance criteria

- Article body on a wide viewport reads at a comfortable line length, not edge-to-edge.
- A 15-source event renders as a scannable list of titled, favicon-marked cards.
- A 10-related-event sidebar can be scanned at a glance for date + category without reading prose.
- All changes are CSS/template; no schema changes (favicons can be a sync-time fetch or runtime fetch behind a cache).
