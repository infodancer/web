-- M5: preserve a question's prior status across a soft-delete/restore cycle.
--
-- A question can be locked (imports default here) or open (native default).
-- An admin can also soft-delete a question (status = 'deleted'). Without a
-- record of what the status was before the delete, restoring would have to
-- pick a fixed destination, losing the lock/open distinction.
--
-- prev_status is set by DeleteQuestion (status -> 'deleted', prev_status =
-- old status) and cleared by RestoreQuestion (status = COALESCE(prev_status,
-- 'open'), prev_status = NULL). Outside of that round-trip it stays NULL.

-- +goose Up

ALTER TABLE faq_questions
    ADD COLUMN prev_status faq_question_status;

-- +goose Down

ALTER TABLE faq_questions
    DROP COLUMN IF EXISTS prev_status;
