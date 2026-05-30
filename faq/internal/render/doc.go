// Package render loads and executes the faq module's HTML templates.
//
// Templates live in two places:
//
//   - The module's embedded templates/ directory ships sensible defaults
//     for every page and partial the handlers render.
//   - The host application may pass an fs.FS via faq.Config.Templates
//     to override individual templates by filename. Host overrides win
//     per-template: consumer can replace just _answer_card.gohtml without
//     having to fork the whole layout.
//
// Two rendering shapes match osg's pattern (DESIGN.md §9, M3 decisions):
//
//   - Full pages live under templates/pages/ and execute through the
//     base.gohtml layout, which renders the "content" block defined by
//     the page.
//   - Partials live under templates/partials/ and render plain (no
//     layout). HTMX-driven endpoints return these directly.
//
// Detection is by template name: any name starting with "partials/" is
// a plain render; everything else is a layout render.
package render
