-- +goose Up

-- is_public is the opt-out listing flag: every community site wants "don't
-- show my profile" and only the module can gate its own public page, so the
-- flag is core rather than a host extension. true (listed) is the default;
-- unlisted profiles 404 on the public page for everyone but the owner and
-- admins. Card() is unaffected -- bylines next to published content stay
-- valid because publishing is public by the act.
ALTER TABLE profile_profiles
    ADD COLUMN is_public boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE profile_profiles
    DROP COLUMN is_public;
