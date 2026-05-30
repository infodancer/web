-- v0.1 schema for the faq module. See DESIGN.md §§4-4.3 for rationale.
--
-- Convention: every table and type is prefixed `faq_` so the module's schema
-- can coexist with the host application's schema in a shared database.

-- +goose Up

-- ---------------------------------------------------------------------------
-- Enums
-- ---------------------------------------------------------------------------

CREATE TYPE faq_question_status AS ENUM ('open', 'locked', 'deleted');

CREATE TYPE faq_vote_value AS ENUM ('up', 'down');

-- ---------------------------------------------------------------------------
-- Sources (one row per import source)
-- ---------------------------------------------------------------------------

CREATE TABLE faq_sources (
    id             BIGSERIAL PRIMARY KEY,
    source_domain  TEXT UNIQUE NOT NULL,
    display_name   TEXT NOT NULL,
    license        TEXT NOT NULL,
    dump_date      DATE NOT NULL,
    imported_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    post_count     INTEGER NOT NULL DEFAULT 0
);

-- ---------------------------------------------------------------------------
-- Local users (OIDC subjects observed posting / voting / commenting)
-- ---------------------------------------------------------------------------

CREATE TABLE faq_users (
    id            BIGSERIAL PRIMARY KEY,
    sub_hash      BYTEA UNIQUE NOT NULL,   -- sha256(iss || ':' || sub), 32 bytes
    display_name  TEXT NOT NULL,
    is_admin      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Imported authors (attribution stubs; never auto-linked to faq_users)
-- ---------------------------------------------------------------------------

CREATE TABLE faq_imported_authors (
    id                BIGSERIAL PRIMARY KEY,
    source_id         BIGINT NOT NULL REFERENCES faq_sources(id),
    source_user_id    BIGINT NOT NULL,         -- upstream user id (per source)
    source_account_id BIGINT,                  -- upstream account id (cross-source)
    display_name      TEXT NOT NULL,
    profile_url       TEXT,
    UNIQUE (source_id, source_user_id)
);

CREATE INDEX faq_imported_authors_account_idx
    ON faq_imported_authors (source_account_id)
    WHERE source_account_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Tags (merged across imports by name; see DESIGN.md §5.8)
-- ---------------------------------------------------------------------------

CREATE TABLE faq_tags (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT UNIQUE NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Questions
-- ---------------------------------------------------------------------------

CREATE TABLE faq_questions (
    id                    BIGSERIAL PRIMARY KEY,
    title                 TEXT NOT NULL,
    body_md               TEXT NOT NULL,
    body_html             TEXT NOT NULL,
    status                faq_question_status NOT NULL DEFAULT 'open',
    accepted_answer_id    BIGINT,  -- FK added after faq_answers, deferred

    -- local authorship
    author_user_id        BIGINT REFERENCES faq_users(id),

    -- imported provenance (nullable for native questions; all set together)
    source_id             BIGINT REFERENCES faq_sources(id),
    source_post_id        BIGINT,
    source_url            TEXT,
    imported_author_id    BIGINT REFERENCES faq_imported_authors(id),
    locally_modified      BOOLEAN NOT NULL DEFAULT FALSE,
    needs_review          BOOLEAN NOT NULL DEFAULT FALSE,

    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    edited_at             TIMESTAMPTZ,
    last_editor_user_id   BIGINT REFERENCES faq_users(id),
    edit_count            INTEGER NOT NULL DEFAULT 0,
    closed_at             TIMESTAMPTZ,
    closed_by_user_id     BIGINT REFERENCES faq_users(id),
    close_reason          TEXT,

    search_tsv            tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(body_md, '')), 'B')
    ) STORED,

    UNIQUE (source_id, source_post_id)
);

CREATE INDEX faq_questions_status_idx  ON faq_questions (status);
CREATE INDEX faq_questions_created_idx ON faq_questions (created_at DESC);
CREATE INDEX faq_questions_source_idx  ON faq_questions (source_id) WHERE source_id IS NOT NULL;
CREATE INDEX faq_questions_review_idx  ON faq_questions (source_id) WHERE needs_review;
CREATE INDEX faq_questions_search_idx  ON faq_questions USING GIN (search_tsv);

-- ---------------------------------------------------------------------------
-- Answers
-- ---------------------------------------------------------------------------

CREATE TABLE faq_answers (
    id                    BIGSERIAL PRIMARY KEY,
    question_id           BIGINT NOT NULL REFERENCES faq_questions(id) ON DELETE CASCADE,
    body_md               TEXT NOT NULL,
    body_html             TEXT NOT NULL,

    author_user_id        BIGINT REFERENCES faq_users(id),

    source_id             BIGINT REFERENCES faq_sources(id),
    source_post_id        BIGINT,
    source_url            TEXT,
    imported_author_id    BIGINT REFERENCES faq_imported_authors(id),
    imported_score        INTEGER,                     -- frozen imported baseline
    local_score           INTEGER NOT NULL DEFAULT 0,  -- trigger-maintained
    locally_modified      BOOLEAN NOT NULL DEFAULT FALSE,
    needs_review          BOOLEAN NOT NULL DEFAULT FALSE,

    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    edited_at             TIMESTAMPTZ,
    last_editor_user_id   BIGINT REFERENCES faq_users(id),
    edit_count            INTEGER NOT NULL DEFAULT 0,

    UNIQUE (source_id, source_post_id)
);

CREATE INDEX faq_answers_question_idx
    ON faq_answers (question_id, (coalesce(imported_score, 0) + local_score) DESC);
CREATE INDEX faq_answers_review_idx
    ON faq_answers (source_id) WHERE needs_review;

-- ---------------------------------------------------------------------------
-- Deferred FK back from questions to answers (for accepted_answer_id).
-- Deferred so importer phases can insert in either order within a tx.
-- ---------------------------------------------------------------------------

ALTER TABLE faq_questions
    ADD CONSTRAINT faq_questions_accepted_fk
    FOREIGN KEY (accepted_answer_id) REFERENCES faq_answers(id)
    DEFERRABLE INITIALLY DEFERRED;

-- ---------------------------------------------------------------------------
-- Question / tag join
-- ---------------------------------------------------------------------------

CREATE TABLE faq_question_tags (
    question_id BIGINT NOT NULL REFERENCES faq_questions(id) ON DELETE CASCADE,
    tag_id      BIGINT NOT NULL REFERENCES faq_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (question_id, tag_id)
);

CREATE INDEX faq_question_tags_tag_idx ON faq_question_tags (tag_id);

-- ---------------------------------------------------------------------------
-- Votes (answer-only in v0.1; question votes arrive in v0.2)
-- ---------------------------------------------------------------------------

CREATE TABLE faq_votes (
    answer_id   BIGINT NOT NULL REFERENCES faq_answers(id) ON DELETE CASCADE,
    user_id     BIGINT NOT NULL REFERENCES faq_users(id) ON DELETE CASCADE,
    value       faq_vote_value NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (answer_id, user_id)
);

-- ---------------------------------------------------------------------------
-- Comments (on a question OR an answer, never both)
-- ---------------------------------------------------------------------------

CREATE TABLE faq_comments (
    id                  BIGSERIAL PRIMARY KEY,
    question_id         BIGINT REFERENCES faq_questions(id) ON DELETE CASCADE,
    answer_id           BIGINT REFERENCES faq_answers(id)   ON DELETE CASCADE,
    body_md             TEXT NOT NULL,
    body_html           TEXT NOT NULL,

    author_user_id      BIGINT REFERENCES faq_users(id),

    source_id           BIGINT REFERENCES faq_sources(id),
    source_post_id      BIGINT,
    imported_author_id  BIGINT REFERENCES faq_imported_authors(id),

    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    edited_at           TIMESTAMPTZ,
    last_editor_user_id BIGINT REFERENCES faq_users(id),
    edit_count          INTEGER NOT NULL DEFAULT 0,

    CONSTRAINT faq_comments_one_parent CHECK ((question_id IS NULL) <> (answer_id IS NULL)),
    UNIQUE (source_id, source_post_id)
);

CREATE INDEX faq_comments_question_idx ON faq_comments (question_id, created_at) WHERE question_id IS NOT NULL;
CREATE INDEX faq_comments_answer_idx   ON faq_comments (answer_id,   created_at) WHERE answer_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Vote score maintenance trigger.
--
-- Maintains faq_answers.local_score as the running sum over faq_votes for
-- that answer (+1 for 'up', -1 for 'down'). Handles INSERT, UPDATE of value,
-- and DELETE atomically.
-- ---------------------------------------------------------------------------

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION faq_apply_vote_delta() RETURNS TRIGGER AS $$
DECLARE
    delta INTEGER := 0;
BEGIN
    IF TG_OP = 'INSERT' THEN
        delta := CASE WHEN NEW.value = 'up' THEN 1 ELSE -1 END;
        UPDATE faq_answers SET local_score = local_score + delta WHERE id = NEW.answer_id;
    ELSIF TG_OP = 'UPDATE' AND NEW.value IS DISTINCT FROM OLD.value THEN
        delta := CASE WHEN NEW.value = 'up' THEN 1 ELSE -1 END
               - CASE WHEN OLD.value = 'up' THEN 1 ELSE -1 END;
        UPDATE faq_answers SET local_score = local_score + delta WHERE id = NEW.answer_id;
    ELSIF TG_OP = 'DELETE' THEN
        delta := -1 * CASE WHEN OLD.value = 'up' THEN 1 ELSE -1 END;
        UPDATE faq_answers SET local_score = local_score + delta WHERE id = OLD.answer_id;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER faq_votes_score_trigger
AFTER INSERT OR UPDATE OF value OR DELETE ON faq_votes
FOR EACH ROW EXECUTE FUNCTION faq_apply_vote_delta();

-- ---------------------------------------------------------------------------
-- Vote eligibility trigger.
--
-- Rejects self-votes and votes on answers whose parent question is not 'open'.
-- The handler layer also checks these and returns 403 instead of 500; this
-- trigger is the floor for direct-SQL paths.
-- ---------------------------------------------------------------------------

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION faq_check_vote_eligibility() RETURNS TRIGGER AS $$
DECLARE
    ans   faq_answers%ROWTYPE;
    qstat faq_question_status;
BEGIN
    SELECT * INTO ans FROM faq_answers WHERE id = NEW.answer_id;
    IF ans.author_user_id = NEW.user_id THEN
        RAISE EXCEPTION 'faq: self-vote not allowed';
    END IF;
    SELECT status INTO qstat FROM faq_questions WHERE id = ans.question_id;
    IF qstat <> 'open' THEN
        RAISE EXCEPTION 'faq: cannot vote on answer to non-open question';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER faq_votes_eligibility_trigger
BEFORE INSERT OR UPDATE OF value ON faq_votes
FOR EACH ROW EXECUTE FUNCTION faq_check_vote_eligibility();

-- +goose Down

DROP TRIGGER IF EXISTS faq_votes_eligibility_trigger ON faq_votes;
DROP TRIGGER IF EXISTS faq_votes_score_trigger ON faq_votes;
DROP FUNCTION IF EXISTS faq_check_vote_eligibility();
DROP FUNCTION IF EXISTS faq_apply_vote_delta();

DROP TABLE IF EXISTS faq_comments;
DROP TABLE IF EXISTS faq_votes;
DROP TABLE IF EXISTS faq_question_tags;

ALTER TABLE IF EXISTS faq_questions DROP CONSTRAINT IF EXISTS faq_questions_accepted_fk;

DROP TABLE IF EXISTS faq_answers;
DROP TABLE IF EXISTS faq_questions;
DROP TABLE IF EXISTS faq_tags;
DROP TABLE IF EXISTS faq_imported_authors;
DROP TABLE IF EXISTS faq_users;
DROP TABLE IF EXISTS faq_sources;

DROP TYPE IF EXISTS faq_vote_value;
DROP TYPE IF EXISTS faq_question_status;
