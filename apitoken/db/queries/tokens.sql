-- name: CreateToken :one
INSERT INTO apitoken_tokens (user_key, name, token_hash, display_prefix, scopes)
VALUES (@user_key, @name, @token_hash, @display_prefix, @scopes)
RETURNING *;

-- name: GetActiveTokenByHash :one
-- The authentication lookup. Revoked rows never authenticate.
SELECT * FROM apitoken_tokens WHERE token_hash = @token_hash AND revoked_at IS NULL;

-- name: TouchToken :exec
-- Record use at most once a minute per token, so a busy client does not turn
-- every request into a write.
UPDATE apitoken_tokens SET last_used_at = now()
WHERE id = @id AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');

-- name: ListTokensForUser :many
-- Every token the user has minted, revoked ones included, newest first.
SELECT * FROM apitoken_tokens WHERE user_key = @user_key ORDER BY created_at DESC, id DESC;

-- name: RevokeToken :execrows
-- Revoke one of the user's own live tokens. Scoped by user_key, so another
-- user's token id behaves exactly like an unknown one.
UPDATE apitoken_tokens SET revoked_at = now()
WHERE id = @id AND user_key = @user_key AND revoked_at IS NULL;

-- name: DeleteTokensForUser :execrows
-- The host's account-deletion hook.
DELETE FROM apitoken_tokens WHERE user_key = @user_key;
