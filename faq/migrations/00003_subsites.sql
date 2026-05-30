-- M6a: subsites — named slices of the corpus surfacing native + imported
-- content under a shared slug. See DESIGN.md §18.4.
--
-- A subsite is a query-time view, not a partition: imported questions live
-- in the global pool and surface in a subsite via faq_subsite_tags
-- (any-tag-match); native questions get faq_questions.subsite_id assigned
-- at create time. Pre-existing native questions keep subsite_id = NULL and
-- continue to appear only on the global /q list — they are not partitioned
-- into any subsite until an admin re-scopes them.

-- +goose Up

CREATE TABLE faq_subsites (
    id           BIGSERIAL PRIMARY KEY,
    slug         TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A subsite includes any question with at least one tag attached here.
-- Many-to-many: a tag can belong to multiple subsites; a subsite has many
-- tags. ON DELETE CASCADE on both sides — dropping a subsite removes its
-- rules, dropping a tag removes its membership.
CREATE TABLE faq_subsite_tags (
    subsite_id BIGINT NOT NULL REFERENCES faq_subsites(id) ON DELETE CASCADE,
    tag_id     BIGINT NOT NULL REFERENCES faq_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (subsite_id, tag_id)
);

CREATE INDEX faq_subsite_tags_tag_idx ON faq_subsite_tags (tag_id);

-- Native questions get a subsite at create time. Imported rows keep this
-- NULL — their subsite membership is computed via the tag-rule path.
ALTER TABLE faq_questions
    ADD COLUMN subsite_id BIGINT REFERENCES faq_subsites(id);

CREATE INDEX faq_questions_subsite_idx
    ON faq_questions (subsite_id)
    WHERE subsite_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS faq_questions_subsite_idx;
ALTER TABLE faq_questions DROP COLUMN IF EXISTS subsite_id;
DROP TABLE IF EXISTS faq_subsite_tags;
DROP TABLE IF EXISTS faq_subsites;
