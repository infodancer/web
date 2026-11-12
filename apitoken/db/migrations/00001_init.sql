-- +goose Up
-- Personal access tokens. Only the SHA-256 of each token is stored; the token
-- itself is shown to its owner once, at creation. display_prefix is the start
-- of the random part, kept so the owner can tell tokens apart.
--
-- user_key is the host's stable user identifier. The module never references
-- the host's users table, so the host calls DeleteForUser when an account
-- goes.
--
-- Tokens do not expire; one lives until it is revoked. Revoked rows are kept
-- for audit and never authenticate.
CREATE TABLE apitoken_tokens (
  id             bigserial PRIMARY KEY,
  user_key       text NOT NULL CHECK (user_key <> ''),
  name           text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
  token_hash     bytea NOT NULL UNIQUE CHECK (length(token_hash) = 32),
  display_prefix text NOT NULL,
  scopes         text[] NOT NULL CHECK (cardinality(scopes) > 0),
  created_at     timestamptz NOT NULL DEFAULT now(),
  last_used_at   timestamptz,
  revoked_at     timestamptz
);

CREATE INDEX apitoken_tokens_user ON apitoken_tokens (user_key, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS apitoken_tokens;
