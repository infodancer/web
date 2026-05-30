-- M6c: explicit per-question curation into subsites.
--
-- A subsite's visible question set in M6a was the union of "native rows
-- whose subsite_id points here" and "imported rows whose tags match a
-- rule in faq_subsite_tags". M6c adds a third branch: an admin-managed
-- (subsite_id, question_id) join table that includes individual
-- questions explicitly, regardless of tag rules. Additive only — an
-- explicit row never *removes* a question that a tag rule would have
-- included. (DESIGN.md §17 M6c, §18.4 D8.)
--
-- Composite PK + CASCADE on both FKs: drop a subsite or a question and
-- the curation links go with it. Per-subsite reads (the dominant access
-- pattern) hit the PK index directly; the question-side reverse lookup
-- powers the canonical-recompute query and the "which subsites does
-- this question belong to" detail-page block.

-- +goose Up

CREATE TABLE faq_subsite_questions (
    subsite_id  BIGINT NOT NULL REFERENCES faq_subsites(id)  ON DELETE CASCADE,
    question_id BIGINT NOT NULL REFERENCES faq_questions(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (subsite_id, question_id)
);

CREATE INDEX faq_subsite_questions_question_idx
    ON faq_subsite_questions (question_id);

-- +goose Down

DROP INDEX IF EXISTS faq_subsite_questions_question_idx;
DROP TABLE IF EXISTS faq_subsite_questions;
