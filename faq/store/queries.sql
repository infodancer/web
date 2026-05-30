-- v0.1 query set for the faq module store layer.
--
-- Bulk-insert paths (import tooling -> faq_questions/faq_answers) use
-- pgx.CopyFrom directly and are not defined here.

-- ---------------------------------------------------------------------------
-- Sources
-- ---------------------------------------------------------------------------

-- name: UpsertSource :one
INSERT INTO faq_sources (source_domain, display_name, license, dump_date, post_count)
VALUES (@source_domain, @display_name, @license, @dump_date, @post_count)
ON CONFLICT (source_domain) DO UPDATE
    SET display_name = EXCLUDED.display_name,
        license      = EXCLUDED.license,
        dump_date    = EXCLUDED.dump_date,
        imported_at  = now()
RETURNING *;

-- name: GetSourceByDomain :one
SELECT * FROM faq_sources WHERE source_domain = @source_domain;

-- name: GetSourceByID :one
SELECT * FROM faq_sources WHERE id = @id;

-- name: ListSources :many
SELECT * FROM faq_sources ORDER BY display_name;

-- name: UpdateSourcePostCount :exec
UPDATE faq_sources SET post_count = @post_count WHERE id = @id;

-- ---------------------------------------------------------------------------
-- Users
-- ---------------------------------------------------------------------------

-- name: UpsertUser :one
INSERT INTO faq_users (sub_hash, display_name)
VALUES (@sub_hash, @display_name)
ON CONFLICT (sub_hash) DO UPDATE
    SET display_name = EXCLUDED.display_name,
        last_seen_at = now()
RETURNING *;

-- name: GetUserBySubHash :one
SELECT * FROM faq_users WHERE sub_hash = @sub_hash;

-- name: GetUserByID :one
SELECT * FROM faq_users WHERE id = @id;

-- name: SetUserAdmin :exec
UPDATE faq_users SET is_admin = @is_admin WHERE id = @id;

-- ---------------------------------------------------------------------------
-- Imported authors
-- ---------------------------------------------------------------------------

-- name: UpsertImportedAuthor :one
INSERT INTO faq_imported_authors (source_id, source_user_id, source_account_id, display_name, profile_url)
VALUES (@source_id, @source_user_id, @source_account_id, @display_name, @profile_url)
ON CONFLICT (source_id, source_user_id) DO UPDATE
    SET source_account_id = EXCLUDED.source_account_id,
        display_name      = EXCLUDED.display_name,
        profile_url       = EXCLUDED.profile_url
RETURNING *;

-- name: GetImportedAuthor :one
SELECT * FROM faq_imported_authors WHERE id = @id;

-- ---------------------------------------------------------------------------
-- Tags
-- ---------------------------------------------------------------------------

-- name: GetOrCreateTag :one
INSERT INTO faq_tags (name, description)
VALUES (@name, @description)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: GetTagByName :one
SELECT * FROM faq_tags WHERE name = @name;

-- name: ListTags :many
SELECT t.*, COUNT(qt.question_id)::BIGINT AS question_count
FROM faq_tags t
LEFT JOIN faq_question_tags qt ON qt.tag_id = t.id
LEFT JOIN faq_questions q ON q.id = qt.question_id AND q.status <> 'deleted'
GROUP BY t.id
ORDER BY question_count DESC, t.name ASC;

-- name: ListTagsForQuestion :many
SELECT t.* FROM faq_tags t
JOIN faq_question_tags qt ON qt.tag_id = t.id
WHERE qt.question_id = @question_id
ORDER BY t.name;

-- name: AttachTagToQuestion :exec
INSERT INTO faq_question_tags (question_id, tag_id)
VALUES (@question_id, @tag_id)
ON CONFLICT DO NOTHING;

-- name: DetachTagFromQuestion :exec
DELETE FROM faq_question_tags WHERE question_id = @question_id AND tag_id = @tag_id;

-- ---------------------------------------------------------------------------
-- Questions (read)
-- ---------------------------------------------------------------------------

-- name: GetQuestion :one
SELECT * FROM faq_questions WHERE id = @id AND status <> 'deleted';

-- name: GetQuestionIncludingDeleted :one
SELECT * FROM faq_questions WHERE id = @id;

-- name: ListQuestionsNewest :many
SELECT * FROM faq_questions
WHERE status <> 'deleted'
ORDER BY created_at DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: ListQuestionsByVotes :many
-- Sorts by the best-answer score; questions with no answers fall to the end.
SELECT q.*, COALESCE(best.score, 0)::INTEGER AS best_answer_score
FROM faq_questions q
LEFT JOIN LATERAL (
    SELECT MAX(COALESCE(a.imported_score, 0) + a.local_score) AS score
    FROM faq_answers a
    WHERE a.question_id = q.id
) best ON TRUE
WHERE q.status <> 'deleted'
ORDER BY best_answer_score DESC NULLS LAST, q.created_at DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: ListQuestionsUnanswered :many
SELECT q.* FROM faq_questions q
WHERE q.status <> 'deleted'
  AND NOT EXISTS (SELECT 1 FROM faq_answers a WHERE a.question_id = q.id)
ORDER BY q.created_at DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: ListQuestionsByTag :many
SELECT q.* FROM faq_questions q
JOIN faq_question_tags qt ON qt.question_id = q.id
JOIN faq_tags t ON t.id = qt.tag_id
WHERE t.name = @tag_name AND q.status <> 'deleted'
ORDER BY q.created_at DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: SearchQuestions :many
SELECT q.*,
       ts_rank_cd('{0.1, 0.2, 0.4, 1.0}', q.search_tsv, query) AS rank
FROM faq_questions q,
     plainto_tsquery('english', @search_query) query
WHERE q.status <> 'deleted'
  AND q.search_tsv @@ query
ORDER BY rank DESC, q.created_at DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: CountQuestions :one
SELECT COUNT(*)::BIGINT FROM faq_questions WHERE status <> 'deleted';

-- name: CountQuestionsByTag :one
SELECT COUNT(*)::BIGINT FROM faq_questions q
JOIN faq_question_tags qt ON qt.question_id = q.id
JOIN faq_tags t ON t.id = qt.tag_id
WHERE t.name = @tag_name AND q.status <> 'deleted';

-- name: ListQuestionsForSitemap :many
-- Stable id ordering across pagination of sub-sitemaps.
SELECT id, title, COALESCE(edited_at, created_at) AS lastmod
FROM faq_questions
WHERE status <> 'deleted'
ORDER BY id ASC
LIMIT @limit_n OFFSET @offset_n;

-- ---------------------------------------------------------------------------
-- Questions (write)
-- ---------------------------------------------------------------------------

-- name: CreateQuestion :one
INSERT INTO faq_questions (title, body_md, body_html, author_user_id)
VALUES (@title, @body_md, @body_html, @author_user_id)
RETURNING *;

-- name: CreateImportedQuestion :one
INSERT INTO faq_questions (
    title, body_md, body_html, status,
    source_id, source_post_id, source_url, imported_author_id,
    needs_review, created_at
) VALUES (
    @title, @body_md, @body_html, 'locked',
    @source_id, @source_post_id, @source_url, @imported_author_id,
    @needs_review, @created_at
)
ON CONFLICT (source_id, source_post_id) DO UPDATE
    SET title              = EXCLUDED.title,
        body_md            = EXCLUDED.body_md,
        body_html          = EXCLUDED.body_html,
        imported_author_id = EXCLUDED.imported_author_id,
        needs_review       = EXCLUDED.needs_review
RETURNING *;

-- name: EditQuestion :exec
UPDATE faq_questions
SET body_md             = @body_md,
    body_html           = @body_html,
    title               = @title,
    edited_at           = now(),
    last_editor_user_id = @last_editor_user_id,
    edit_count          = edit_count + 1,
    locally_modified    = TRUE,
    updated_at          = now()
WHERE id = @id;

-- name: SetQuestionStatus :exec
UPDATE faq_questions
SET status            = @status,
    closed_at         = CASE WHEN @status::faq_question_status IN ('locked', 'deleted') THEN now() ELSE NULL END,
    closed_by_user_id = @closed_by_user_id,
    close_reason      = @close_reason,
    updated_at        = now()
WHERE id = @id;

-- ---------------------------------------------------------------------------
-- Answers (read)
-- ---------------------------------------------------------------------------

-- name: GetAnswer :one
SELECT * FROM faq_answers WHERE id = @id;

-- name: ListAnswersForQuestion :many
SELECT *, (COALESCE(imported_score, 0) + local_score)::INTEGER AS total_score
FROM faq_answers
WHERE question_id = @question_id
ORDER BY total_score DESC, created_at ASC;

-- ---------------------------------------------------------------------------
-- Answers (write)
-- ---------------------------------------------------------------------------

-- name: CreateAnswer :one
INSERT INTO faq_answers (question_id, body_md, body_html, author_user_id)
VALUES (@question_id, @body_md, @body_html, @author_user_id)
RETURNING *;

-- name: CreateImportedAnswer :one
INSERT INTO faq_answers (
    question_id, body_md, body_html,
    source_id, source_post_id, source_url, imported_author_id,
    imported_score, needs_review, created_at
) VALUES (
    @question_id, @body_md, @body_html,
    @source_id, @source_post_id, @source_url, @imported_author_id,
    @imported_score, @needs_review, @created_at
)
ON CONFLICT (source_id, source_post_id) DO UPDATE
    SET body_md            = EXCLUDED.body_md,
        body_html          = EXCLUDED.body_html,
        imported_author_id = EXCLUDED.imported_author_id,
        imported_score     = EXCLUDED.imported_score,
        needs_review       = EXCLUDED.needs_review
RETURNING *;

-- name: EditAnswer :exec
UPDATE faq_answers
SET body_md             = @body_md,
    body_html           = @body_html,
    edited_at           = now(),
    last_editor_user_id = @last_editor_user_id,
    edit_count          = edit_count + 1,
    locally_modified    = TRUE,
    updated_at          = now()
WHERE id = @id;

-- ---------------------------------------------------------------------------
-- Votes
-- ---------------------------------------------------------------------------

-- name: GetUserVote :one
SELECT * FROM faq_votes WHERE answer_id = @answer_id AND user_id = @user_id;

-- name: UpsertVote :one
INSERT INTO faq_votes (answer_id, user_id, value)
VALUES (@answer_id, @user_id, @value)
ON CONFLICT (answer_id, user_id) DO UPDATE
    SET value = EXCLUDED.value,
        created_at = now()
RETURNING *;

-- name: DeleteVote :exec
DELETE FROM faq_votes WHERE answer_id = @answer_id AND user_id = @user_id;

-- ---------------------------------------------------------------------------
-- Comments
-- ---------------------------------------------------------------------------

-- name: ListCommentsForQuestion :many
SELECT * FROM faq_comments
WHERE question_id = @question_id
ORDER BY created_at ASC;

-- name: ListCommentsForAnswer :many
SELECT * FROM faq_comments
WHERE answer_id = @answer_id
ORDER BY created_at ASC;

-- name: CreateQuestionComment :one
INSERT INTO faq_comments (question_id, body_md, body_html, author_user_id)
VALUES (@question_id, @body_md, @body_html, @author_user_id)
RETURNING *;

-- name: CreateAnswerComment :one
INSERT INTO faq_comments (answer_id, body_md, body_html, author_user_id)
VALUES (@answer_id, @body_md, @body_html, @author_user_id)
RETURNING *;

-- name: CreateImportedComment :one
INSERT INTO faq_comments (
    question_id, answer_id, body_md, body_html,
    source_id, source_post_id, imported_author_id, created_at
) VALUES (
    @question_id, @answer_id, @body_md, @body_html,
    @source_id, @source_post_id, @imported_author_id, @created_at
)
ON CONFLICT (source_id, source_post_id) DO UPDATE
    SET body_md            = EXCLUDED.body_md,
        body_html           = EXCLUDED.body_html,
        imported_author_id  = EXCLUDED.imported_author_id
RETURNING *;

-- name: EditComment :exec
UPDATE faq_comments
SET body_md             = @body_md,
    body_html           = @body_html,
    edited_at           = now(),
    last_editor_user_id = @last_editor_user_id,
    edit_count          = edit_count + 1
WHERE id = @id;

-- name: GetComment :one
SELECT * FROM faq_comments WHERE id = @id;

-- ---------------------------------------------------------------------------
-- Batch lookups used by the read-path list/detail views (M3).
-- ---------------------------------------------------------------------------

-- name: CountAnswersByQuestionIDs :many
-- For each input question_id, returns the answer count. Missing IDs
-- (no answers) are absent from the result; callers default them to 0.
SELECT question_id, COUNT(*)::INTEGER AS answer_count
FROM faq_answers
WHERE question_id = ANY(@question_ids::BIGINT[])
GROUP BY question_id;

-- name: BestScoreByQuestionIDs :many
-- For each input question_id, the best answer's total score
-- (imported_score + local_score). Missing IDs are absent; default 0.
SELECT question_id,
       COALESCE(MAX(COALESCE(imported_score, 0) + local_score), 0)::INTEGER
       AS best_score
FROM faq_answers
WHERE question_id = ANY(@question_ids::BIGINT[])
GROUP BY question_id;

-- name: TagsByQuestionIDs :many
-- Returns (question_id, tag_name) pairs for the input question IDs, sorted
-- so the handler can collect them into a stable per-question slice with a
-- single pass.
SELECT qt.question_id, t.name AS tag_name
FROM faq_question_tags qt
JOIN faq_tags t ON t.id = qt.tag_id
WHERE qt.question_id = ANY(@question_ids::BIGINT[])
ORDER BY qt.question_id, t.name;

-- name: SourcesByIDs :many
-- Resolve faq_sources rows for a set of ids (used to render attribution
-- on imported questions in list and detail views).
SELECT id, source_domain, display_name, license
FROM faq_sources
WHERE id = ANY(@source_ids::BIGINT[]);

-- ---------------------------------------------------------------------------
-- Accept-answer (M4.8)
-- ---------------------------------------------------------------------------

-- name: SetAcceptedAnswer :exec
-- Sets faq_questions.accepted_answer_id. Pass NULL to clear (toggle off).
UPDATE faq_questions SET accepted_answer_id = @accepted_answer_id WHERE id = @id;

-- ---------------------------------------------------------------------------
-- Moderation (M5)
-- ---------------------------------------------------------------------------

-- name: LockQuestion :exec
-- Admin lock. Only meaningful on 'open' questions; a no-op WHERE clause
-- keeps a double-lock from clobbering closed_at / closed_by_user_id.
UPDATE faq_questions
SET status            = 'locked',
    closed_at         = now(),
    closed_by_user_id = @closed_by_user_id,
    close_reason      = @close_reason,
    updated_at        = now()
WHERE id = @id AND status = 'open';

-- name: UnlockQuestion :exec
-- Admin unlock. Clears the closed_* metadata along with the status flip.
UPDATE faq_questions
SET status            = 'open',
    closed_at         = NULL,
    closed_by_user_id = NULL,
    close_reason      = NULL,
    updated_at        = now()
WHERE id = @id AND status = 'locked';

-- name: DeleteQuestion :exec
-- Soft-delete. Captures the pre-delete status into prev_status so a later
-- RestoreQuestion can put the row back exactly where it was (locked
-- questions stay locked after restore; open stay open). Skips rows already
-- deleted so prev_status isn't overwritten with 'deleted'.
UPDATE faq_questions
SET prev_status       = status,
    status            = 'deleted',
    closed_at         = now(),
    closed_by_user_id = @closed_by_user_id,
    close_reason      = @close_reason,
    updated_at        = now()
WHERE id = @id AND status <> 'deleted';

-- name: RestoreQuestion :exec
-- Reverses DeleteQuestion. Falls back to 'open' if prev_status is NULL
-- (older rows from before the M5 migration). Clears closed_* fields when
-- restoring to 'open'; leaves them when restoring to 'locked'.
UPDATE faq_questions
SET status            = COALESCE(prev_status, 'open'),
    prev_status       = NULL,
    closed_at         = CASE WHEN COALESCE(prev_status, 'open') = 'locked' THEN closed_at ELSE NULL END,
    closed_by_user_id = CASE WHEN COALESCE(prev_status, 'open') = 'locked' THEN closed_by_user_id ELSE NULL END,
    close_reason      = CASE WHEN COALESCE(prev_status, 'open') = 'locked' THEN close_reason ELSE NULL END,
    updated_at        = now()
WHERE id = @id AND status = 'deleted';

-- name: ListNeedsReviewQuestions :many
-- Admin queue: questions whose body OR any of their answers is flagged
-- needs_review. Deleted questions are filtered out. Optional source filter:
-- pass source_id = 0 to disable the filter (sqlc's @-params don't allow
-- nullable scalars without a separate query, and 0 is never a valid id).
SELECT q.*
FROM faq_questions q
WHERE q.status <> 'deleted'
  AND (@source_id::BIGINT = 0 OR q.source_id = @source_id)
  AND (
        q.needs_review
        OR EXISTS (
            SELECT 1 FROM faq_answers a
            WHERE a.question_id = q.id AND a.needs_review
        )
      )
ORDER BY q.created_at DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: CountNeedsReviewQuestions :one
SELECT COUNT(*)::BIGINT FROM faq_questions q
WHERE q.status <> 'deleted'
  AND (@source_id::BIGINT = 0 OR q.source_id = @source_id)
  AND (
        q.needs_review
        OR EXISTS (
            SELECT 1 FROM faq_answers a
            WHERE a.question_id = q.id AND a.needs_review
        )
      );

-- name: ClearNeedsReviewCascade :exec
-- Marking a question reviewed also clears the flag on all of its answers
-- (per the M5 decision: reviewing the question counts as reviewing its
-- answers). Two statements in one query keep them atomic at the
-- transaction boundary the handler already opens.
WITH cleared_question AS (
    UPDATE faq_questions SET needs_review = FALSE WHERE faq_questions.id = @id
    RETURNING faq_questions.id AS qid
)
UPDATE faq_answers
SET needs_review = FALSE
WHERE question_id = (SELECT qid FROM cleared_question);

-- name: ListDeletedQuestions :many
-- Admin recovery list: only rows where status = 'deleted'. Most recent
-- delete first (closed_at is set by DeleteQuestion).
SELECT * FROM faq_questions
WHERE status = 'deleted'
ORDER BY closed_at DESC NULLS LAST, id DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: CountDeletedQuestions :one
SELECT COUNT(*)::BIGINT FROM faq_questions WHERE status = 'deleted';

-- ---------------------------------------------------------------------------
-- Subsites (M6a, DESIGN.md §18.4)
--
-- A subsite is a named slice of the corpus. Native questions belong to one
-- subsite via faq_questions.subsite_id. Imported questions are pulled into a
-- subsite by tag rule (faq_subsite_tags). The two branches are disjoint in
-- practice (native rows have source_id IS NULL; the import branch filters
-- source_id IS NOT NULL), so UNION ALL is safe and cheaper than UNION.
-- ---------------------------------------------------------------------------

-- name: CreateSubsite :one
INSERT INTO faq_subsites (slug, display_name, description)
VALUES (@slug, @display_name, @description)
RETURNING *;

-- name: GetSubsiteByID :one
SELECT * FROM faq_subsites WHERE id = @id;

-- name: GetSubsiteBySlug :one
SELECT * FROM faq_subsites WHERE slug = @slug;

-- name: ListSubsites :many
-- Admin view: every subsite plus a question-count column (native +
-- tag-matched imported + explicit curation, deduped). Sorted
-- alphabetically by display_name.
SELECT s.*, (
    SELECT COUNT(*)::BIGINT FROM (
        SELECT q.id
        FROM faq_questions q
        WHERE q.status <> 'deleted' AND q.subsite_id = s.id
        UNION
        SELECT q.id
        FROM faq_questions q
        WHERE q.status <> 'deleted' AND q.source_id IS NOT NULL
          AND EXISTS (
              SELECT 1 FROM faq_question_tags qt
              JOIN faq_subsite_tags st ON st.tag_id = qt.tag_id
              WHERE qt.question_id = q.id AND st.subsite_id = s.id
          )
        UNION
        SELECT q.id
        FROM faq_questions q
        JOIN faq_subsite_questions sq ON sq.question_id = q.id
        WHERE q.status <> 'deleted' AND sq.subsite_id = s.id
    ) counted
) AS question_count
FROM faq_subsites s
ORDER BY s.display_name;

-- name: UpdateSubsite :exec
UPDATE faq_subsites
SET slug = @slug, display_name = @display_name, description = @description
WHERE id = @id;

-- name: DeleteSubsite :exec
DELETE FROM faq_subsites WHERE id = @id;

-- ---------------------------------------------------------------------------
-- Subsite tag rules
-- ---------------------------------------------------------------------------

-- name: AttachTagToSubsite :exec
INSERT INTO faq_subsite_tags (subsite_id, tag_id)
VALUES (@subsite_id, @tag_id)
ON CONFLICT DO NOTHING;

-- name: DetachTagFromSubsite :exec
DELETE FROM faq_subsite_tags WHERE subsite_id = @subsite_id AND tag_id = @tag_id;

-- name: ListTagRulesForSubsite :many
-- Tag rules attached to a subsite, with each tag's global question count
-- and the per-subsite match count (how many tagged questions actually live
-- in this subsite via this rule). Admin-only.
SELECT t.*, (
    SELECT COUNT(*)::BIGINT
    FROM faq_question_tags qt
    JOIN faq_questions q ON q.id = qt.question_id
    WHERE qt.tag_id = t.id AND q.status <> 'deleted'
) AS global_question_count
FROM faq_tags t
JOIN faq_subsite_tags st ON st.tag_id = t.id
WHERE st.subsite_id = @subsite_id
ORDER BY t.name;

-- ---------------------------------------------------------------------------
-- Subsite read path — list / count / search
-- ---------------------------------------------------------------------------

-- name: ListSubsiteQuestionsNewest :many
(
    SELECT q.* FROM faq_questions q
    WHERE q.status <> 'deleted' AND q.subsite_id = @subsite_id
)
UNION
(
    SELECT q.* FROM faq_questions q
    WHERE q.status <> 'deleted'
      AND q.source_id IS NOT NULL
      AND EXISTS (
          SELECT 1 FROM faq_question_tags qt
          JOIN faq_subsite_tags st ON st.tag_id = qt.tag_id
          WHERE qt.question_id = q.id AND st.subsite_id = @subsite_id
      )
)
UNION
(
    SELECT q.* FROM faq_questions q
    JOIN faq_subsite_questions sq ON sq.question_id = q.id
    WHERE q.status <> 'deleted' AND sq.subsite_id = @subsite_id
)
ORDER BY created_at DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: ListSubsiteQuestionsByVotes :many
WITH scoped AS (
    SELECT q.id FROM faq_questions q
    WHERE q.status <> 'deleted' AND q.subsite_id = @subsite_id
    UNION
    SELECT q.id FROM faq_questions q
    WHERE q.status <> 'deleted'
      AND q.source_id IS NOT NULL
      AND EXISTS (
          SELECT 1 FROM faq_question_tags qt
          JOIN faq_subsite_tags st ON st.tag_id = qt.tag_id
          WHERE qt.question_id = q.id AND st.subsite_id = @subsite_id
      )
    UNION
    SELECT q.id FROM faq_questions q
    JOIN faq_subsite_questions sq ON sq.question_id = q.id
    WHERE q.status <> 'deleted' AND sq.subsite_id = @subsite_id
)
SELECT q.*, COALESCE(best.score, 0)::INTEGER AS best_answer_score
FROM faq_questions q
JOIN scoped ON scoped.id = q.id
LEFT JOIN LATERAL (
    SELECT MAX(COALESCE(a.imported_score, 0) + a.local_score) AS score
    FROM faq_answers a WHERE a.question_id = q.id
) best ON TRUE
ORDER BY best_answer_score DESC NULLS LAST, q.created_at DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: ListSubsiteQuestionsUnanswered :many
WITH scoped AS (
    SELECT q.id FROM faq_questions q
    WHERE q.status <> 'deleted' AND q.subsite_id = @subsite_id
    UNION
    SELECT q.id FROM faq_questions q
    WHERE q.status <> 'deleted'
      AND q.source_id IS NOT NULL
      AND EXISTS (
          SELECT 1 FROM faq_question_tags qt
          JOIN faq_subsite_tags st ON st.tag_id = qt.tag_id
          WHERE qt.question_id = q.id AND st.subsite_id = @subsite_id
      )
    UNION
    SELECT q.id FROM faq_questions q
    JOIN faq_subsite_questions sq ON sq.question_id = q.id
    WHERE q.status <> 'deleted' AND sq.subsite_id = @subsite_id
)
SELECT q.* FROM faq_questions q
JOIN scoped ON scoped.id = q.id
WHERE NOT EXISTS (SELECT 1 FROM faq_answers a WHERE a.question_id = q.id)
ORDER BY q.created_at DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: ListSubsiteQuestionsByTag :many
-- Intersection: questions in the subsite AND tagged @tag_name.
WITH scoped AS (
    SELECT q.id FROM faq_questions q
    WHERE q.status <> 'deleted' AND q.subsite_id = @subsite_id
    UNION
    SELECT q.id FROM faq_questions q
    WHERE q.status <> 'deleted'
      AND q.source_id IS NOT NULL
      AND EXISTS (
          SELECT 1 FROM faq_question_tags qt
          JOIN faq_subsite_tags st ON st.tag_id = qt.tag_id
          WHERE qt.question_id = q.id AND st.subsite_id = @subsite_id
      )
    UNION
    SELECT q.id FROM faq_questions q
    JOIN faq_subsite_questions sq ON sq.question_id = q.id
    WHERE q.status <> 'deleted' AND sq.subsite_id = @subsite_id
)
SELECT q.* FROM faq_questions q
JOIN scoped ON scoped.id = q.id
JOIN faq_question_tags qt ON qt.question_id = q.id
JOIN faq_tags t ON t.id = qt.tag_id
WHERE t.name = @tag_name
ORDER BY q.created_at DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: SearchSubsiteQuestions :many
WITH scoped AS (
    SELECT q.id FROM faq_questions q
    WHERE q.status <> 'deleted' AND q.subsite_id = @subsite_id
    UNION
    SELECT q.id FROM faq_questions q
    WHERE q.status <> 'deleted'
      AND q.source_id IS NOT NULL
      AND EXISTS (
          SELECT 1 FROM faq_question_tags qt
          JOIN faq_subsite_tags st ON st.tag_id = qt.tag_id
          WHERE qt.question_id = q.id AND st.subsite_id = @subsite_id
      )
    UNION
    SELECT q.id FROM faq_questions q
    JOIN faq_subsite_questions sq ON sq.question_id = q.id
    WHERE q.status <> 'deleted' AND sq.subsite_id = @subsite_id
)
SELECT q.*,
       ts_rank_cd('{0.1, 0.2, 0.4, 1.0}', q.search_tsv, query) AS rank
FROM faq_questions q
JOIN scoped ON scoped.id = q.id,
     plainto_tsquery('english', @search_query) query
WHERE q.search_tsv @@ query
ORDER BY rank DESC, q.created_at DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: CountSubsiteQuestions :one
SELECT COUNT(*)::BIGINT FROM (
    SELECT q.id FROM faq_questions q
    WHERE q.status <> 'deleted' AND q.subsite_id = @subsite_id
    UNION
    SELECT q.id FROM faq_questions q
    WHERE q.status <> 'deleted'
      AND q.source_id IS NOT NULL
      AND EXISTS (
          SELECT 1 FROM faq_question_tags qt
          JOIN faq_subsite_tags st ON st.tag_id = qt.tag_id
          WHERE qt.question_id = q.id AND st.subsite_id = @subsite_id
      )
    UNION
    SELECT q.id FROM faq_questions q
    JOIN faq_subsite_questions sq ON sq.question_id = q.id
    WHERE q.status <> 'deleted' AND sq.subsite_id = @subsite_id
) counted;

-- name: CountSubsiteQuestionsByTag :one
SELECT COUNT(*)::BIGINT FROM faq_questions q
JOIN faq_question_tags qt ON qt.question_id = q.id
JOIN faq_tags t ON t.id = qt.tag_id
WHERE t.name = @tag_name AND q.status <> 'deleted'
  AND (
        q.subsite_id = @subsite_id
        OR (q.source_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM faq_question_tags qt2
            JOIN faq_subsite_tags st ON st.tag_id = qt2.tag_id
            WHERE qt2.question_id = q.id AND st.subsite_id = @subsite_id
        ))
        OR EXISTS (
            SELECT 1 FROM faq_subsite_questions sq
            WHERE sq.question_id = q.id AND sq.subsite_id = @subsite_id
        )
      );

-- ---------------------------------------------------------------------------
-- Subsite question-membership checks (route 404 + see-also)
-- ---------------------------------------------------------------------------

-- name: IsQuestionInSubsite :one
-- Returns 1 if @question_id is reachable from @subsite_id (native,
-- tag-rule match, or explicit curation), 0 otherwise. Cheaper than
-- re-running the full list query: handlers call this on every
-- subsite-prefixed detail load to decide 404 vs. render.
SELECT EXISTS (
    SELECT 1 FROM faq_questions q
    WHERE q.id = @question_id AND q.status <> 'deleted'
      AND (
            q.subsite_id = @subsite_id
            OR (q.source_id IS NOT NULL AND EXISTS (
                SELECT 1 FROM faq_question_tags qt
                JOIN faq_subsite_tags st ON st.tag_id = qt.tag_id
                WHERE qt.question_id = q.id AND st.subsite_id = @subsite_id
            ))
            OR EXISTS (
                SELECT 1 FROM faq_subsite_questions sq
                WHERE sq.question_id = q.id AND sq.subsite_id = @subsite_id
            )
          )
)::BOOLEAN AS in_subsite;

-- name: ListSubsitesForQuestion :many
-- Every subsite the question belongs to: native ownership, tag-rule
-- match, or explicit curation. Used by the cross-subsite "see also"
-- block on the detail page (D8.6).
SELECT DISTINCT s.* FROM faq_subsites s
WHERE EXISTS (
    SELECT 1 FROM faq_questions q
    WHERE q.id = @question_id AND q.status <> 'deleted'
      AND (
            q.subsite_id = s.id
            OR (q.source_id IS NOT NULL AND EXISTS (
                SELECT 1 FROM faq_question_tags qt
                JOIN faq_subsite_tags st ON st.tag_id = qt.tag_id
                WHERE qt.question_id = q.id AND st.subsite_id = s.id
            ))
            OR EXISTS (
                SELECT 1 FROM faq_subsite_questions sq
                WHERE sq.question_id = q.id AND sq.subsite_id = s.id
            )
          )
)
ORDER BY s.display_name;

-- ---------------------------------------------------------------------------
-- Subsite-scoped tags + recent (landing pages)
-- ---------------------------------------------------------------------------

-- name: ListTopTagsForSubsite :many
-- Tags actually used by questions reachable from this subsite (native,
-- tag-matched, or explicitly curated), sorted by per-subsite question
-- count. Backs the subsite landing's tag chips and the subsite
-- tags-index page.
SELECT t.*, COUNT(*)::BIGINT AS question_count
FROM faq_tags t
JOIN faq_question_tags qt ON qt.tag_id = t.id
JOIN faq_questions q ON q.id = qt.question_id
WHERE q.status <> 'deleted'
  AND (
        q.subsite_id = @subsite_id
        OR (q.source_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM faq_question_tags qt2
            JOIN faq_subsite_tags st ON st.tag_id = qt2.tag_id
            WHERE qt2.question_id = q.id AND st.subsite_id = @subsite_id
        ))
        OR EXISTS (
            SELECT 1 FROM faq_subsite_questions sq
            WHERE sq.question_id = q.id AND sq.subsite_id = @subsite_id
        )
      )
GROUP BY t.id
ORDER BY question_count DESC, t.name ASC
LIMIT @limit_n;

-- name: ListSubsiteQuestionsForSitemap :many
-- Stable id ordering across pagination of a subsite's sub-sitemap.
WITH scoped AS (
    SELECT q.id FROM faq_questions q
    WHERE q.status <> 'deleted' AND q.subsite_id = @subsite_id
    UNION
    SELECT q.id FROM faq_questions q
    WHERE q.status <> 'deleted'
      AND q.source_id IS NOT NULL
      AND EXISTS (
          SELECT 1 FROM faq_question_tags qt
          JOIN faq_subsite_tags st ON st.tag_id = qt.tag_id
          WHERE qt.question_id = q.id AND st.subsite_id = @subsite_id
      )
    UNION
    SELECT q.id FROM faq_questions q
    JOIN faq_subsite_questions sq ON sq.question_id = q.id
    WHERE q.status <> 'deleted' AND sq.subsite_id = @subsite_id
)
SELECT q.id, q.title, COALESCE(q.edited_at, q.created_at) AS lastmod
FROM faq_questions q
JOIN scoped ON scoped.id = q.id
ORDER BY q.id ASC
LIMIT @limit_n OFFSET @offset_n;

-- ---------------------------------------------------------------------------
-- Subsite write path
-- ---------------------------------------------------------------------------

-- name: CreateNativeQuestionInSubsite :one
-- Native questions canonicalize to their owning subsite (D8.1 + D8.3), so
-- canonical_subsite_id mirrors subsite_id at insert time. The recompute
-- pass would eventually catch this, but populating up-front means the
-- first detail-page render is already correct.
INSERT INTO faq_questions (title, body_md, body_html, author_user_id, subsite_id, canonical_subsite_id)
VALUES (@title, @body_md, @body_html, @author_user_id, @subsite_id, @subsite_id)
RETURNING *;

-- name: SetQuestionSubsite :exec
-- Admin re-scope of a native question. No-op on imported rows (caller
-- enforces; the SQL accepts but the WHERE guards source_id IS NULL so
-- imports cannot have subsite_id written even by an admin slip). Updates
-- canonical_subsite_id in lockstep — natives always canonicalize to their
-- owning subsite (D8.3).
UPDATE faq_questions
SET subsite_id = @subsite_id, canonical_subsite_id = @subsite_id, updated_at = now()
WHERE id = @id AND source_id IS NULL;

-- name: GetCanonicalSubsiteSlugForQuestion :one
-- Returns the slug of a question's canonical subsite. pgx.ErrNoRows
-- means canonical_subsite_id is NULL (or the question is missing), and
-- the caller should treat the global /q URL as canonical.
SELECT s.slug
FROM faq_questions q
JOIN faq_subsites s ON s.id = q.canonical_subsite_id
WHERE q.id = @id;

-- name: RecomputeCanonicalSubsites :exec
-- Per D8.3 (revised, extended in M6c). Sets canonical_subsite_id for
-- every question:
--   * native (subsite_id IS NOT NULL) → its owning subsite
--   * otherwise: count the distinct subsites the question matches via
--     tag rules OR explicit faq_subsite_questions curation.
--       - exactly one → that subsite
--       - zero or many → NULL (global is canonical)
--
-- The IS DISTINCT FROM guard skips no-op rows so the statement only
-- writes rows whose canonical actually changed. Safe to call repeatedly;
-- intended trigger points are after subsite seeding, after an import,
-- and after admin curation changes.
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
            SELECT count(*) AS subsite_count,
                   min(subsite_id) AS only_subsite_id
              FROM (
                  SELECT st.subsite_id
                    FROM faq_question_tags qt
                    JOIN faq_subsite_tags st ON st.tag_id = qt.tag_id
                   WHERE qt.question_id = q.id
                  UNION
                  SELECT sq.subsite_id
                    FROM faq_subsite_questions sq
                   WHERE sq.question_id = q.id
              ) matches
        ) mc ON true
  ) derived
 WHERE q.id = derived.question_id
   AND q.canonical_subsite_id IS DISTINCT FROM derived.new_canonical;

-- ---------------------------------------------------------------------------
-- Subsite explicit curation (M6c, DESIGN.md §17)
--
-- Admins can pin individual questions into a subsite regardless of tag
-- rules. Membership is additive — an explicit row never removes a
-- question that a tag rule would have included. The visibility queries
-- above (ListSubsiteQuestions*, IsQuestionInSubsite, ListSubsites,
-- ListSubsitesForQuestion, ListTopTagsForSubsite, CountSubsiteQuestions*,
-- ListSubsiteQuestionsForSitemap) check this third branch.
-- ---------------------------------------------------------------------------

-- name: IncludeQuestionInSubsite :exec
INSERT INTO faq_subsite_questions (subsite_id, question_id)
VALUES (@subsite_id, @question_id)
ON CONFLICT DO NOTHING;

-- name: RemoveQuestionFromSubsite :exec
DELETE FROM faq_subsite_questions
WHERE subsite_id = @subsite_id AND question_id = @question_id;

-- name: ListExplicitSubsiteQuestions :many
-- Admin view: every question explicitly pinned into this subsite,
-- newest-pinned first. Soft-deleted questions are filtered out so
-- admin pages don't show ghosts.
SELECT q.*
FROM faq_questions q
JOIN faq_subsite_questions sq ON sq.question_id = q.id
WHERE sq.subsite_id = @subsite_id AND q.status <> 'deleted'
ORDER BY sq.created_at DESC, q.id DESC
LIMIT @limit_n OFFSET @offset_n;

-- name: CountExplicitSubsiteQuestions :one
SELECT COUNT(*)::BIGINT
FROM faq_subsite_questions sq
JOIN faq_questions q ON q.id = sq.question_id
WHERE sq.subsite_id = @subsite_id AND q.status <> 'deleted';

-- name: ListExplicitSubsitesForQuestion :many
-- Used by the admin question detail view to highlight which subsites a
-- question is explicitly curated into (distinct from native ownership
-- and tag-rule matches, which are surfaced separately).
SELECT s.*
FROM faq_subsites s
JOIN faq_subsite_questions sq ON sq.subsite_id = s.id
WHERE sq.question_id = @question_id
ORDER BY s.display_name;

-- name: IsQuestionExplicitlyInSubsite :one
SELECT EXISTS (
    SELECT 1 FROM faq_subsite_questions
    WHERE subsite_id = @subsite_id AND question_id = @question_id
)::BOOLEAN AS in_subsite;
