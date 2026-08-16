-- name: CreateInvitation :one
INSERT INTO claim_invitations (
  entity_type, entity_ref, invited_email, token_hash, created_by, expires_at
) VALUES (
  @entity_type, @entity_ref, @invited_email, @token_hash, @created_by, @expires_at
)
RETURNING *;

-- name: GetInvitationByTokenHash :one
-- The whole verification path. Presented tokens are hashed and looked up here,
-- so there is no application-level secret comparison to get wrong: a wrong
-- token simply finds no row. State (expired, consumed, revoked) is returned
-- rather than filtered so the caller can tell the cases apart and say something
-- useful instead of a flat "invalid link".
SELECT * FROM claim_invitations WHERE token_hash = @token_hash;

-- name: GetLiveInvitationForEntity :one
-- The outstanding invitation for one entity, if any. Matches the partial unique
-- index, so this returns at most one row by construction.
SELECT * FROM claim_invitations
WHERE entity_type = @entity_type
  AND entity_ref = @entity_ref
  AND consumed_at IS NULL
  AND revoked_at IS NULL;

-- name: ListLiveInvitationsForEmail :many
-- Every live invitation waiting on an address, for the deferred
-- account-creation matcher. Callers must confirm the address is verified by the
-- identity provider before acting on these.
SELECT * FROM claim_invitations
WHERE lower(invited_email) = lower(@invited_email::text)
  AND consumed_at IS NULL
  AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: ConsumeInvitation :one
-- Single use, enforced in the WHERE clause rather than by a read-then-write in
-- application code: two simultaneous claims cannot both match, so the second
-- returns no row instead of silently overwriting the first claimant.
UPDATE claim_invitations
SET consumed_at = NOW(), consumed_by = @consumed_by
WHERE token_hash = @token_hash
  AND consumed_at IS NULL
  AND revoked_at IS NULL
  AND expires_at > NOW()
RETURNING *;

-- name: RevokeLiveInvitationsForEntity :execrows
-- Invalidates any outstanding invitation for an entity. Called before issuing a
-- replacement so the old link stops working the moment a new one is generated.
UPDATE claim_invitations
SET revoked_at = NOW()
WHERE entity_type = @entity_type
  AND entity_ref = @entity_ref
  AND consumed_at IS NULL
  AND revoked_at IS NULL;

-- name: ListInvitationsForEntity :many
-- Full history for one entity, newest first, for an admin audit view.
SELECT * FROM claim_invitations
WHERE entity_type = @entity_type AND entity_ref = @entity_ref
ORDER BY created_at DESC;
