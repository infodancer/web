# infodancer/web/profile — design

*The component shipped in [#53](https://github.com/infodancer/web/pull/53); the
scope rationale lives in [#52](https://github.com/infodancer/web/issues/52).
This document records the part that isn't obvious from the code: what belongs
in profile versus in the host, and the model for site-specific extension
(designed 2026-06-12, [#58](https://github.com/infodancer/web/issues/58)).
The `Sections` hook described in §3 shipped in
[#60](https://github.com/infodancer/web/issues/60).*

## 1. Scope: the common denominator, not the whole persona

profile owns the public-persona fields every community site in the suite
needs: a unique handle, a display name, a short markdown bio, an avatar
(upload via web/media, deterministic identicon fallback), and bio links —
plus the surfaces over them: the edit form, the public profile page, and the
embeddable `Card()` other components render next to user-authored content.

Two constraints define the boundary:

- **Users are the host's, profiles are peripheral.** A users table is the
  host's FK hub (campaigns, characters, claimed authors all point into it);
  a component that owned it would stop being mountable and start being the
  site. profile therefore keys rows by an opaque, host-supplied user key and
  owns no auth, sessions, or registration.
- **The basics are common; the rest is not — by design.** Sites are
  *expected* to carry additional persona fields: a gaming site wants a home
  location and campaign visibility; a book site wants author-claim status
  and links to claimed works. Those fields are the site's domain, stored in
  the site's tables. profile must neither absorb them (a migration per site
  per field, forever) nor pretend they don't exist (the pre-component state:
  basics on one page, everything else somewhere unrelated).

The design question this document answers: how does a site present its
additional fields *coherently with* the basics — one profile page, one edit
surface — without duplicating profile's code and without modifying profile
for every field a site invents?

## 2. Rejected mechanisms

Two obvious approaches fail the constraints:

**Template/slot overrides with a form-merge hook.** Let the host inject
markup into profile's edit form (via `Config.Templates` or named blocks) and
hand unknown POST fields back through a `SaveExtra` callback. This keeps
literally one `<form>`, but one form with two owners is the trap: merged
validation, merged error rendering, and partial-save semantics the first
time a host field fails while profile's fields succeed. Template overrides
also drift against profile upgrades — every host carries a fork of the form.

**A field-descriptor registry.** Hosts register
`FieldSpec{Key, Kind, Validate, Load, Store}` and profile renders the extra
fields generically. Maximally uniform, zero host templates — and a treadmill.
The widget vocabulary inevitably grows (a map picker for home location, a
claim-workflow control, a date, an image), and every new `Kind` is a profile
change: exactly the "modify profile for every possible field" outcome this
design exists to avoid. Acceptable for plain text and enums only, which is
too small a ceiling for real sites.

## 3. The extension model: sections

The extension unit is a **section**, not a field. A section is a host-owned,
self-contained block — title, view markup, optionally edit markup — that
profile *places* on its pages but never parses, validates, or stores.

profile gains exactly one hook, ever:

```go
// Sections supplies host-owned profile sections, rendered after profile's
// own basics on the public page (ViewHTML) and the edit page (EditHTML).
// Called per request; the host gates per-viewer visibility itself.
Sections func(ctx context.Context, r *http.Request, userKey string) []Section

type Section struct {
    Title    string
    ViewHTML template.HTML // "" omits the section from the public page
    EditHTML template.HTML // "" omits the section from the edit page
}
```

The contract:

- **Each section is an htmx island.** The host renders it with its own
  templates, and its inputs post to *host* endpoints — save-per-section,
  the same display↔edit lifecycle the suite already uses (mdedit fields,
  osg's inventory table). profile's own form saves profile's fields;
  a section's form saves the section's. No merged validation, no
  partial-save ambiguity, no foreign fields riding profile's POST.
- **Storage stays host-side**, in host tables keyed by the host's user id.
  profile's schema never changes for a site field; the FK-hub invariant
  from #52 is preserved.
- **The host owns auth and CSRF for section endpoints**, exactly as it does
  for every other route it serves. profile's same-origin guard covers only
  profile's own writes.
- **Coherence is a visual contract, not shared code.** profile ships a small
  `profile-section` CSS vocabulary (section heading, field rows, save
  affordance) chained through the `--app-*` tokens, and renders its own
  basics as the first section of the same shape — so a host section is
  pixel-consistent with the block above it without sharing templates.
- **`Card()` stays core-only.** The small embeddable unit (avatar + name)
  does not grow site fields; components that want more compose it with
  their own markup.

Sectioned pages with per-section save are also the honest UX: "Basics" save
together, "Gaming" saves together, "Author" saves together — the settings
shape users already know — rather than one super-form pretending two owners
are one.

### What this costs

A site with literally one extra text field pays a whole section for it.
Accepted: one seam used uniformly beats a second mechanism (the descriptor
registry) maintained alongside for small cases.

## 4. Implementation status

The `Sections` hook is implemented
([#60](https://github.com/infodancer/web/issues/60)); the first consumer is
osg's port off its bespoke persona, whose host sections carry the gaming
fields (home location, player tags) and the bestiary-contributor link.
sf's author-claim surface remains a planned consumer. The hook is additive:
a nil `Sections` changes nothing.
