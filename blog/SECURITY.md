# Security Policy

## Reporting a Vulnerability

Use [GitHub's private vulnerability reporting](https://github.com/infodancer/web/blog/security/advisories/new) for any security-relevant issue. Public issue tracking is fine for non-security bugs.

## Scope

- The blog module's HTTP surface (post create/edit/publish, comments, admin).
- Markdown rendering (goldmark + bluemonday sanitizer policy). XSS in rendered post or comment bodies.
- Authorization checks on edit/delete/publish flows.
- CSRF on write endpoints.

Out of scope:
- Consumer site code that mounts the module.
- `infodancer/oidclient` / `infodancer/webauth` (report there).
