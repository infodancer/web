-- +goose Up
CREATE TABLE IF NOT EXISTS subscribers (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	email      TEXT    NOT NULL UNIQUE,
	created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	synced_at  TEXT
);

-- +goose Down
DROP TABLE IF EXISTS subscribers;
