-- v0.1 schema for the notify module. See DESIGN.md for rationale.
--
-- Convention: every table is prefixed `notify_` so the module's schema can
-- coexist with the host application's schema in a shared database, and the
-- goose history lives in its own version table (see Migrate).
--
-- Identity is recorded as opaque (issuer, subject) strings — NEVER a foreign
-- key into a host table. This keeps the schema self-contained so a host can
-- point notify at its own database today and, if it ever wants to, a separate
-- one tomorrow, with no cross-table coupling.

-- +goose Up

CREATE TABLE notify_events (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source          TEXT        NOT NULL,
    type            TEXT        NOT NULL,
    title           TEXT        NOT NULL,
    severity        TEXT        NOT NULL DEFAULT 'info',
    audience        TEXT        NOT NULL DEFAULT 'admin',
    actor_issuer    TEXT,
    actor_subject   TEXT,
    actor_label     TEXT,
    subject_issuer  TEXT,
    subject_subject TEXT,
    payload         JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT notify_events_severity_chk
        CHECK (severity IN ('info', 'notice', 'warn', 'alert')),
    CONSTRAINT notify_events_audience_chk
        CHECK (audience IN ('admin', 'user'))
);

-- created_at DESC is the dominant feed query; id breaks ties and drives the
-- keyset paginator. audience leads so the admin feed scans only admin rows.
CREATE INDEX notify_events_feed_idx
    ON notify_events (audience, created_at DESC, id DESC);
CREATE INDEX notify_events_type_idx   ON notify_events (type);
CREATE INDEX notify_events_source_idx ON notify_events (source);

-- Per-admin read cursor: one row per admin identity, not one per event.
-- unread = count of events newer than last_seen_at; mark-seen advances it.
CREATE TABLE notify_admin_read_cursor (
    issuer       TEXT        NOT NULL,
    subject      TEXT        NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issuer, subject)
);

-- +goose Down
DROP TABLE notify_admin_read_cursor;
DROP TABLE notify_events;
