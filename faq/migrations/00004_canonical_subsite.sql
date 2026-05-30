-- M6c prep: per-question canonical subsite for SEO.
--
-- D8.3 (revised): when a question maps unambiguously to a single subsite,
-- search engines should see that subsite URL as the canonical address —
-- not the global /q/{id} URL. This produces tighter topic clusters and
-- lets subsite landing pages accrue link equity for their topic area.
--
-- canonical_subsite_id captures the answer:
--   * subsite_id IS NOT NULL  → canonical = subsite_id (native question, D8.1)
--   * subsite_id IS NULL, exactly one matching subsite via tag rules
--                              → canonical = that subsite
--   * subsite_id IS NULL, zero or multiple matches
--                              → canonical = NULL (global /q is canonical)
--
-- Maintained by Module.RecomputeCanonicalSubsites; safe to re-run.

-- +goose Up

ALTER TABLE faq_questions
    ADD COLUMN canonical_subsite_id BIGINT REFERENCES faq_subsites(id) ON DELETE SET NULL;

CREATE INDEX faq_questions_canonical_subsite_idx
    ON faq_questions (canonical_subsite_id)
    WHERE canonical_subsite_id IS NOT NULL;

-- Initial population. The recompute logic is duplicated here because the
-- store query that exposes it to Go is added in the same release; running
-- it once during the migration means a fresh deployment has the column
-- populated before the first request lands.
UPDATE faq_questions q
   SET canonical_subsite_id = derived.new_canonical
  FROM (
      SELECT q.id AS question_id,
             CASE
                 WHEN q.subsite_id IS NOT NULL THEN q.subsite_id
                 WHEN mc.subsite_count = 1 THEN mc.only_subsite_id
                 ELSE NULL
             END AS new_canonical
        FROM faq_questions q
        LEFT JOIN LATERAL (
            SELECT count(DISTINCT st.subsite_id) AS subsite_count,
                   min(st.subsite_id) AS only_subsite_id
              FROM faq_question_tags qt
              JOIN faq_subsite_tags st ON st.tag_id = qt.tag_id
             WHERE qt.question_id = q.id
        ) mc ON true
  ) derived
 WHERE q.id = derived.question_id
   AND q.canonical_subsite_id IS DISTINCT FROM derived.new_canonical;

-- +goose Down

DROP INDEX IF EXISTS faq_questions_canonical_subsite_idx;
ALTER TABLE faq_questions DROP COLUMN IF EXISTS canonical_subsite_id;
