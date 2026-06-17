-- v0.1 schema for the errnotice module. See README.md for rationale.
--
-- Convention: the table is prefixed `error_notices` (the component name is
-- already in the table name per the suite's table-prefix rule), and the goose
-- history lives in its own version table (errnotice_goose_db_version, see
-- migrate.go) so the module's migrations coexist with the host's schema and
-- other components' migrations in one shared database.
--
-- One row per distinct failure, identified by `fingerprint`: a stable hash over
-- (component, method, normalized path, status, panic class). Repeats of the
-- same logical failure UPSERT into the same row -- count rises and last_seen
-- advances -- so a crash loop is one row with a rising count, not thousands of
-- rows. The fingerprint is a grouping heuristic, not a security boundary.

-- +goose Up

CREATE TABLE error_notices (
    id          BIGSERIAL   PRIMARY KEY,
    fingerprint TEXT        NOT NULL UNIQUE,        -- sha256 hex grouping key
    request_id  TEXT        NOT NULL DEFAULT '',    -- last occurrence's correlation id
    component   TEXT        NOT NULL DEFAULT '',
    method      TEXT        NOT NULL DEFAULT '',
    path        TEXT        NOT NULL DEFAULT '',    -- last occurrence's raw path
    status      INTEGER     NOT NULL,
    panic_value TEXT        NOT NULL DEFAULT '',    -- truncated; '' for a plain 5xx
    stack       TEXT        NOT NULL DEFAULT '',    -- truncated goroutine stack
    first_seen  TIMESTAMPTZ NOT NULL,
    last_seen   TIMESTAMPTZ NOT NULL,
    count       BIGINT      NOT NULL DEFAULT 1
);

-- The admin "recent errors" view lists newest activity first.
CREATE INDEX error_notices_last_seen_idx
    ON error_notices (last_seen DESC);

-- +goose Down
DROP TABLE error_notices;
