-- v0.1 schema for the antibot module.
--
-- Convention: every table is prefixed `antibot_` so the module's schema can
-- coexist with the host application's schema in a shared database, and the
-- goose history lives in its own version table (see Migrate).
--
-- The limiter never stores who submitted. key_hash is HMAC-SHA256 of the IP
-- address or email under the host's secret, so the table holds no personal data
-- and a dump of it identifies nobody. Counting needs equality, not the value.

-- +goose Up
CREATE TABLE antibot_counters (
    form         text        NOT NULL,
    key_kind     text        NOT NULL CHECK (key_kind IN ('ip', 'email')),
    key_hash     bytea       NOT NULL,
    window_start timestamptz NOT NULL,
    hits         integer     NOT NULL DEFAULT 1 CHECK (hits > 0),
    expires_at   timestamptz NOT NULL,
    PRIMARY KEY (form, key_kind, key_hash, window_start)
);

-- Sweep deletes by expiry; without this it scans the table.
CREATE INDEX antibot_counters_expires_at ON antibot_counters (expires_at);

-- +goose Down
DROP TABLE antibot_counters;
