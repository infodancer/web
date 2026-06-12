# Security Policy

## Reporting a Vulnerability

Use [GitHub's private vulnerability reporting](https://github.com/infodancer/web/security/advisories/new) for any security-relevant issue. Public issue tracking is fine for non-security bugs.

## Scope

- The blog module's HTTP surface (post create/edit/publish/archive/delete, image upload, named-blog admin, feeds, sitemap).
- Markdown rendering (the `infodancer/ui/markdown` goldmark + bluemonday pipeline). XSS in rendered post bodies, feed documents, or JSON-LD output.
- Authorization checks on the write and admin flows (author/admin role gates, own-post enforcement).
- CSRF on write endpoints.

Out of scope:
- Consumer site code that mounts the module.
- `infodancer/oidclient` / `infodancer/webauth` (report there).
