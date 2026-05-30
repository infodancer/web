package handler

import (
	"errors"
	"regexp"
)

// reservedSubsiteSlugs are the path segments registered as literal
// top-level routes at the module's mount point. A subsite slug equal
// to any of these would collide with route precedence (Go 1.22 ServeMux
// favors specific patterns, but the wildcard pattern that resolves
// {subsite} would never reach a subsite whose slug shadows a literal
// route). Per DESIGN.md §18.4 D3.
//
// Kept as an unexported package map because v0.1 has no extension
// surface — sf and osg don't add new top-level routes. If a future
// consumer needs to extend it, surface this through a public
// Config.ReservedSubsiteSlugs field on faq.Config.
var reservedSubsiteSlugs = map[string]struct{}{
	"q":           {},
	"tags":        {},
	"search":      {},
	"admin":       {},
	"static":      {},
	"sitemap.xml": {},
	"health":      {},
	"a":           {}, // answer-permalink routes (POST /a/{id}/vote, etc.)
	"comments":    {}, // PATCH /comments/{id}
	"sitemap":     {}, // GET /sitemap/{name}
}

// reservedTagSlugs are the path segments that have specific meaning as
// the second segment after a subsite slug. A tag name equal to one of
// these cannot be reached via the short tag-filter form `/{subsite}/{tag}`
// — the literal route always wins. Per DESIGN.md §18.4 D3.
var reservedTagSlugs = map[string]struct{}{
	"q":      {},
	"tags":   {},
	"search": {},
	"ask":    {},
}

// slugCharPattern matches the shape of a valid slug: lowercase ASCII
// letters, digits, and inner hyphens. Slugs may not start or end with
// a hyphen and may not contain consecutive hyphens. This is the same
// shape imported tags commonly use, so they fit by default.
var slugCharPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// maxSlugLen is the maximum length of a subsite or tag slug.
const maxSlugLen = 64

// errSlugReserved means the slug is well-formed but collides with a
// literal route or sub-route. Distinguished from format errors so the
// admin UI can show a more helpful message.
var errSlugReserved = errors.New("slug is reserved")

// errSlugBadFormat means the slug doesn't match the allowed character
// set or length cap.
var errSlugBadFormat = errors.New("slug must be lowercase a-z, 0-9 and hyphens, 1-64 chars, no leading/trailing hyphen")

// validateSubsiteSlug accepts a candidate subsite slug and returns nil
// if it is both well-formed and not reserved. Callers should report
// the error in user-visible form on the admin create form.
func validateSubsiteSlug(s string) error {
	if !slugCharPattern.MatchString(s) || len(s) > maxSlugLen {
		return errSlugBadFormat
	}
	if _, reserved := reservedSubsiteSlugs[s]; reserved {
		return errSlugReserved
	}
	return nil
}

// validateTagSlug accepts a candidate tag name (which doubles as a URL
// slug in the short tag-filter form) and returns nil if it is both
// well-formed and not reserved at the second-segment position.
//
// Tags created via the ask form that fail this check should be rejected
// before [store.GetOrCreateTag] is called: a tag named "q" would never
// be reachable from a subsite via the short form `/{subsite}/q` (the
// literal /{subsite}/q list route wins). The longer form
// `/{subsite}/tags/{name}` still works, so a reserved-name tag is not
// unreachable from the UI — but allowing it in the first place would
// surprise both authors (their tag click goes to the list page) and
// readers (they can't tell whether `/sub/q` is the list or a tag).
func validateTagSlug(s string) error {
	if !slugCharPattern.MatchString(s) || len(s) > maxSlugLen {
		return errSlugBadFormat
	}
	if _, reserved := reservedTagSlugs[s]; reserved {
		return errSlugReserved
	}
	return nil
}
