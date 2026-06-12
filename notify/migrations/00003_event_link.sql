-- +goose Up

-- Click-through target for the notification: an absolute http(s) URL into the
-- producing site, or '' when the event has nothing to link to. Validated at
-- the write path (Event.normalize), stored as plain text. NOT NULL DEFAULT ''
-- rather than nullable: "no link" has no distinct meaning beyond empty, and a
-- plain string keeps the Go mapping simple.
ALTER TABLE notify_events
    ADD COLUMN link TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE notify_events
    DROP COLUMN link;
