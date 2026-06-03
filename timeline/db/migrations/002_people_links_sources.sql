-- +goose Up

CREATE TABLE people (
    id          BIGSERIAL PRIMARY KEY,
    timeline_id BIGINT NOT NULL REFERENCES timelines(id) ON DELETE CASCADE,
    slug        TEXT NOT NULL,
    name        TEXT NOT NULL,
    sort_name   TEXT NOT NULL DEFAULT '',
    bio         TEXT NOT NULL DEFAULT '',
    born_date   DATE,
    died_date   DATE,
    photo_url   TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(timeline_id, slug)
);

CREATE INDEX idx_people_timeline ON people (timeline_id, sort_name);

CREATE TABLE event_people (
    event_id   BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    person_id  BIGINT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT '',
    explainer  TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    PRIMARY KEY (event_id, person_id)
);

CREATE INDEX idx_event_people_person ON event_people (person_id);

CREATE TABLE event_links (
    event_a_id    BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    event_b_id    BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    relationship  TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (event_a_id, event_b_id),
    CHECK (event_a_id < event_b_id)
);

CREATE INDEX idx_event_links_b ON event_links (event_b_id);

CREATE TABLE event_sources (
    id          BIGSERIAL PRIMARY KEY,
    event_id    BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    url         TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    author      TEXT NOT NULL DEFAULT '',
    citation    TEXT NOT NULL DEFAULT '',
    sort_order  INT NOT NULL DEFAULT 0
);

CREATE INDEX idx_event_sources_event ON event_sources (event_id);

-- +goose Down
DROP TABLE IF EXISTS event_sources;
DROP TABLE IF EXISTS event_links;
DROP TABLE IF EXISTS event_people;
DROP TABLE IF EXISTS people;
