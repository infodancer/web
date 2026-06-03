-- +goose Up

CREATE TABLE timeline_users (
    id           BIGSERIAL PRIMARY KEY,
    sub_hash     BYTEA UNIQUE NOT NULL,   -- sha256(iss || ':' || sub), 32 bytes
    display_name TEXT NOT NULL DEFAULT '',
    is_admin     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS timeline_users;
