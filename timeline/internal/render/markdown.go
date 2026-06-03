package render

import (
	"html/template"

	"github.com/infodancer/ui/markdown"
)

// articleRenderer renders event articles and person bios through
// infodancer/ui/markdown — the suite's single audited goldmark + bluemonday
// pipeline. Rich() enables GFM (tables, strikethrough, autolinks, task lists),
// footnotes, and inline kbd/sub/sup/mark, with bluemonday as the security
// boundary. The renderer is immutable and concurrent-safe; build it once.
var articleRenderer = markdown.New(markdown.Rich())

// Markdown converts authored Markdown to sanitized HTML for templates. It is
// exposed to templates as the {{markdown ...}} function.
func Markdown(md string) template.HTML {
	return articleRenderer.Render(md)
}
