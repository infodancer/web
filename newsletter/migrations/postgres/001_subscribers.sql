-- +goose Up
CREATE TABLE IF NOT EXISTS subscribers (
	id         INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	email      TEXT    NOT NULL UNIQUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	synced_at  TIMESTAMPTZ
);

-- +goose Down
DROP TABLE IF EXISTS subscribers;
