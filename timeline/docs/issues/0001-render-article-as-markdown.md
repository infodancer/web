# 0001: Render `article` field as Markdown

**Status:** open
**Area:** rendering
**Priority:** high

## Problem

`CLAUDE.md` describes the event `article` field as Markdown, but the templates render it as plain text. Both `event_detail.html` and `event_inline.html` use `{{.Event.Article}}`, which goes through Go's `html/template` auto-escape and emits the raw source — including `**bold**`, `## headings`, `> blockquotes`, list bullets, and link syntax — verbatim. Authors writing Markdown see their formatting eaten.

## Proposal

Render `article` (and `summary`, for consistency in the inline lead paragraph) as Markdown.

- Add a Markdown renderer dependency. `goldmark` is the standard pick in the Go ecosystem: actively maintained, CommonMark-compliant, extensible (tables, footnotes, autolinks), produces safe HTML when configured with `html.WithUnsafe()` *disabled*.
- Add a template helper `markdown` that takes a string and returns `template.HTML`. Render once at request time; for hot paths, consider caching the rendered HTML on the event row or in an in-memory cache keyed by event id + updated_at.
- Update templates: `{{.Event.Article | markdown}}` and `{{.Event.Summary | markdown}}` (or apply only to `article` if summary should stay plain — see #0002).
- Sanitize the output. Even with `WithUnsafe()` off, run through `bluemonday.UGCPolicy()` (or equivalent) before marking as safe HTML, so any inline HTML in source data can't escape.

## Out of scope

- Authoring UI for Markdown (this is a rendering issue, not an editor issue).
- Custom shortcodes, embed widgets, footnote styling — separate work if desired.

## Acceptance criteria

- An event whose `article` contains H2 headings, bullets, blockquotes, bold/italic, and links renders with the corresponding HTML structure on the event detail page.
- Output passes a basic XSS check: a hostile `<script>` tag in source data is stripped, not executed.
- No raw Markdown syntax visible to the reader.
