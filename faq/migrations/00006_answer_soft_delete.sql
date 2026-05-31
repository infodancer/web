-- Allow an answer's author (while the question is open) or an admin to
-- soft-delete it. deleted_at NULL = live; non-NULL = hidden from the answer list
-- for everyone except admins, who keep seeing it inline with a Restore control.
--
-- Mirrors the question soft-delete, but answers carry no lock state, so they
-- need no prev_status companion — deleted_at alone is the flag. Deleting the
-- accepted answer clears the question's accepted_answer_id (handled in the
-- delete handler, not here).

-- +goose Up

ALTER TABLE faq_answers
    ADD COLUMN deleted_at TIMESTAMPTZ;

-- +goose Down

ALTER TABLE faq_answers
    DROP COLUMN IF EXISTS deleted_at;
