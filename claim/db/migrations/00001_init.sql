-- +goose Up

-- claim_invitations records an invitation for someone to take ownership of a
-- host entity they do not yet have an account for.
--
-- The module deliberately knows nothing about the host's identity or content
-- tables. It stores strings the host chooses (entity_type/entity_ref for the
-- thing, created_by/consumed_by for the people) and has no foreign keys outside
-- its own schema, so the same component drops into any site's database. That
-- also means a deleted host user leaves the audit trail intact rather than
-- nulling out who issued or accepted an invitation.
CREATE TABLE claim_invitations (
  id            bigserial PRIMARY KEY,

  -- The host's own vocabulary: what kind of thing, and its stable public
  -- identifier. A slug is the intended value for entity_ref rather than a row
  -- id -- it survives being read on its own, and the module cannot resolve a
  -- number against a table it cannot see.
  entity_type   text NOT NULL,
  entity_ref    text NOT NULL,

  -- The address the invitation was addressed to. Recorded here rather than read
  -- back from the host's contact field, which is editable business data that
  -- drifts; this is the audit record of who was actually invited to what, and
  -- what a later account-creation matcher has to match against.
  invited_email text NOT NULL,

  -- SHA-256 of the token. The token itself is never stored, so a database read
  -- (or a backup, or a stray SELECT) yields no working claim links. The
  -- consequence is that the URL can be shown exactly once, at creation.
  token_hash    bytea NOT NULL,

  created_by    text NOT NULL DEFAULT '',
  created_at    timestamptz NOT NULL DEFAULT NOW(),
  expires_at    timestamptz NOT NULL,

  -- Consumed and revoked rows are retained rather than deleted: the history of
  -- who was invited and what became of it is the point of the table.
  consumed_at   timestamptz,
  consumed_by   text NOT NULL DEFAULT '',
  revoked_at    timestamptz,

  CONSTRAINT claim_invitations_entity_type_present CHECK (entity_type <> ''),
  CONSTRAINT claim_invitations_entity_ref_present  CHECK (entity_ref <> ''),
  CONSTRAINT claim_invitations_email_present       CHECK (invited_email <> ''),
  CONSTRAINT claim_invitations_expiry_after_creation CHECK (expires_at > created_at),
  -- A consumed row must say who consumed it, or the audit record is worthless.
  CONSTRAINT claim_invitations_consumed_by_present
    CHECK (consumed_at IS NULL OR consumed_by <> '')
);

-- Tokens are looked up by hash, so this index is both the uniqueness guarantee
-- and the lookup path.
CREATE UNIQUE INDEX claim_invitations_token_hash ON claim_invitations (token_hash);

-- At most one live invitation per entity. Issuing a new link revokes the
-- previous one, and this makes that an invariant the database enforces rather
-- than a rule the application remembers.
CREATE UNIQUE INDEX claim_invitations_one_live
  ON claim_invitations (entity_type, entity_ref)
  WHERE consumed_at IS NULL AND revoked_at IS NULL;

-- Supports the deferred account-creation matcher: given a newly verified
-- address, find the live invitations waiting for it. Case-insensitive because
-- the local part is case-sensitive in principle and nobody treats it that way.
CREATE INDEX claim_invitations_live_email
  ON claim_invitations (lower(invited_email))
  WHERE consumed_at IS NULL AND revoked_at IS NULL;

-- +goose Down
DROP TABLE claim_invitations;
