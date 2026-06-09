-- +goose Up

-- profile_profiles is the component's only table: the public persona for a
-- host-identified user. user_key is the host's opaque, stable identifier
-- (e.g. its users.id as text) -- the component never owns the users table,
-- because that table is the host's FK hub.
--
-- handle is lowercase-normalized in the application layer before storage, so
-- a plain UNIQUE constraint gives case-insensitive uniqueness without
-- requiring the citext extension (a mountable component cannot assume it may
-- CREATE EXTENSION). NULL until the user claims one; claimed handles are
-- immutable except by admin override.
CREATE TABLE profile_profiles (
    user_key     text PRIMARY KEY,
    handle       text UNIQUE,
    display_name text NOT NULL DEFAULT '',
    bio_md       text NOT NULL DEFAULT '',
    avatar_url   text NOT NULL DEFAULT '',
    links        jsonb NOT NULL DEFAULT '[]',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE profile_profiles;
