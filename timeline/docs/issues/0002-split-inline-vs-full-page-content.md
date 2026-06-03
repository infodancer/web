# 0002: Split inline expansion vs full-page content

**Status:** open
**Area:** rendering / UX
**Priority:** high

## Problem

When a user clicks an event on the timeline, the inline expansion (`event_inline.html`) renders **everything**: title, summary, tags, the full `article` body, all people, all related events, and the full source list. For events with long articles this produces a multi-screen wall of text inline on the timeline, defeating the purpose of a scannable lane view.

The full event detail page (`event_detail.html`) exists and already has a "Open full page →" link from the inline view, but most readers never make it that far because the inline view has already shown them the whole entry.

## Proposal

Treat the inline view as a **preview**, the detail page as the **document**.

**Inline view (`event_inline.html`) shows:**
- Date + category chip
- Title (or `headline` if present — see #0004)
- `summary` (1–3 sentences)
- Tags
- "Open full page →" link
- Optional: a small "People: A, B, C" line as plain text — names only, no role/explainer prose

**Inline view does NOT show:**
- `article` body
- Per-person role/explainer paragraphs
- Related events block
- Sources list

**Detail page (`event_detail.html`) shows everything:**
- All inline content
- Full `article` body
- People sidebar with roles and explainers
- Related events sidebar
- Numbered source list

## Rationale

The inline view's job is "is this event worth opening?" Answering that needs the date, the title, and a 1–3 sentence summary. Anything more is friction. Long-form content lives on its own URL where typography, scrolling, and metadata can be designed for reading rather than scanning.

This change is also necessary for the timeline to scale: 200+ events on a single page each capable of expanding to a multi-screen entry is unworkable.

## Acceptance criteria

- Clicking an event in the lane view expands to a compact preview, never longer than ~5 lines of body text.
- "Open full page →" navigates to the detail page, which shows the full article body, sources, and related events.
- An event with no `article` field renders identically to one with a long article in the inline view (the inline view doesn't peek at `article` at all).
