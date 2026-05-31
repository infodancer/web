-- +goose Up

-- Per-recipient delivery rows. An event with a non-empty Recipients slice
-- writes one row here per recipient, each carrying its own read/dismiss state.
-- This is the user counterpart to notify_admin_read_cursor: admins see ~every
-- event so they use a single monotonic cursor, but a user receives only a
-- subset and needs per-event read state.
CREATE TABLE notify_user_notifications (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id          BIGINT      NOT NULL REFERENCES notify_events (id) ON DELETE CASCADE,
    recipient_issuer  TEXT        NOT NULL,
    recipient_subject TEXT        NOT NULL,
    read_at           TIMESTAMPTZ,
    dismissed_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- One delivery row per (event, recipient): a redundant Emit cannot
    -- double-deliver, and ON CONFLICT DO NOTHING makes the fan-out idempotent.
    CONSTRAINT notify_user_notifications_uniq UNIQUE (event_id, recipient_issuer, recipient_subject)
);

-- The dominant query is "this recipient's feed, newest first" with keyset
-- pagination on (created_at DESC, id DESC); recipient leads so the scan touches
-- only one user's rows.
CREATE INDEX notify_user_notifications_feed_idx
    ON notify_user_notifications (recipient_issuer, recipient_subject, created_at DESC, id DESC);

-- +goose Down
DROP TABLE notify_user_notifications;
