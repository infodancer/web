-- Account keys: forms behind a sign-in count submissions per account, since one
-- account posting from many addresses slips under a per-IP limit. As with the
-- other kinds, key_hash is an HMAC of the account id, never the id itself.

-- +goose Up
ALTER TABLE antibot_counters DROP CONSTRAINT antibot_counters_key_kind_check;
ALTER TABLE antibot_counters ADD CONSTRAINT antibot_counters_key_kind_check
    CHECK (key_kind IN ('ip', 'email', 'account'));

-- +goose Down
DELETE FROM antibot_counters WHERE key_kind = 'account';
ALTER TABLE antibot_counters DROP CONSTRAINT antibot_counters_key_kind_check;
ALTER TABLE antibot_counters ADD CONSTRAINT antibot_counters_key_kind_check
    CHECK (key_kind IN ('ip', 'email'));
