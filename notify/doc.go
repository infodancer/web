// Package notify is a reusable notification system for websites.
//
// A host site records events — usually from user actions, such as a new
// account being created — as small, flexible objects in PostgreSQL. Admins
// (the primary audience) see them in a bell + feed; users (a later phase) see
// a personalized subset and can opt into email per type.
//
// The host's integration surface is deliberately small:
//
//   - Producer: construct an [Emitter] and call [Emitter.Emit] at each event
//     point. The default async emitter never blocks the request path and never
//     surfaces a delivery failure to the user flow — a dropped notification
//     must never fail an account creation.
//   - Storage: [NewStore] wraps a pgx pool; [Migrate] installs the schema under
//     a dedicated goose version table so it coexists with the host's own schema.
//   - Admin UI (phase 2): a mountable handler set the host gates with its own
//     auth and mounts under a base path.
//
// Identity is recorded as opaque (issuer, subject) strings, never a foreign
// key into a host table, so the schema stays self-contained.
//
// See DESIGN.md at the repository root for architecture and rationale.
package notify
