# Security

The search module is built to be visibly secure by design. See DESIGN.md §10
for the full posture. Highlights:

- **Two least-privilege DB roles.** A read-write indexer role owns the schema
  and writes the corpus; a read-only query role (used by `serve`) holds
  `SELECT` only and has no write path to the index tables. A query-layer bug
  cannot corrupt the corpus.
- **Injection-safe queries.** All SQL goes through `sqlc`/`pgx` parameter
  binding. User search input is parsed with `websearch_to_tsquery`, which has
  no FTS-syntax injection surface.
- **No third-party HTML served.** Only extracted plain text and our own
  escaped snippets are stored and rendered. Page/Markdown HTML is reduced to
  text at index time, removing the result-rendering XSS surface.
- **Same-origin CSRF guard** on state-changing requests (configurable).
- **Secrets** (DSNs, API keys) come from config/environment, never committed.
- **DoS posture.** Page-size and fan-out clamps; context query timeouts.

## Reporting

Report suspected vulnerabilities privately to the maintainer rather than via a
public issue.
