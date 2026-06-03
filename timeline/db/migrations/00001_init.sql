-- +goose Up

CREATE TABLE timeline_timelines (
    id          BIGSERIAL PRIMARY KEY,
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    start_date  DATE,
    end_date    DATE,
    is_public   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE timeline_categories (
    id          BIGSERIAL PRIMARY KEY,
    timeline_id BIGINT NOT NULL REFERENCES timeline_timelines(id) ON DELETE CASCADE,
    slug        TEXT NOT NULL,
    name        TEXT NOT NULL,
    color       TEXT NOT NULL DEFAULT '#6366f1',
    sort_order  INT NOT NULL DEFAULT 0,
    UNIQUE(timeline_id, slug)
);

CREATE INDEX idx_timeline_categories_timeline ON timeline_categories (timeline_id);

CREATE TABLE timeline_tags (
    id   BIGSERIAL PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

CREATE TABLE timeline_events (
    id             BIGSERIAL PRIMARY KEY,
    timeline_id    BIGINT NOT NULL REFERENCES timeline_timelines(id) ON DELETE CASCADE,
    slug           TEXT NOT NULL,
    title          TEXT NOT NULL,
    summary        TEXT NOT NULL DEFAULT '',
    article        TEXT NOT NULL DEFAULT '',
    event_date     DATE NOT NULL,
    event_end_date DATE,
    date_precision TEXT NOT NULL DEFAULT 'day',
    sort_order     INT NOT NULL DEFAULT 0,
    category_id    BIGINT REFERENCES timeline_categories(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(timeline_id, slug)
);

CREATE INDEX idx_timeline_events_timeline_date ON timeline_events (timeline_id, event_date, sort_order);
CREATE INDEX idx_timeline_events_timeline_category_date ON timeline_events (timeline_id, category_id, event_date);
CREATE INDEX idx_timeline_events_search ON timeline_events USING gin (
    to_tsvector('english', title || ' ' || summary || ' ' || article)
);

CREATE TABLE timeline_event_tags (
    event_id BIGINT NOT NULL REFERENCES timeline_events(id) ON DELETE CASCADE,
    tag_id   BIGINT NOT NULL REFERENCES timeline_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (event_id, tag_id)
);

CREATE INDEX idx_timeline_event_tags_tag ON timeline_event_tags (tag_id);

CREATE TABLE timeline_people (
    id          BIGSERIAL PRIMARY KEY,
    timeline_id BIGINT NOT NULL REFERENCES timeline_timelines(id) ON DELETE CASCADE,
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

CREATE INDEX idx_timeline_people_timeline ON timeline_people (timeline_id, sort_name);

CREATE TABLE timeline_event_people (
    event_id   BIGINT NOT NULL REFERENCES timeline_events(id) ON DELETE CASCADE,
    person_id  BIGINT NOT NULL REFERENCES timeline_people(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT '',
    explainer  TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    PRIMARY KEY (event_id, person_id)
);

CREATE INDEX idx_timeline_event_people_person ON timeline_event_people (person_id);

CREATE TABLE timeline_event_links (
    event_a_id   BIGINT NOT NULL REFERENCES timeline_events(id) ON DELETE CASCADE,
    event_b_id   BIGINT NOT NULL REFERENCES timeline_events(id) ON DELETE CASCADE,
    relationship TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (event_a_id, event_b_id),
    CHECK (event_a_id < event_b_id)
);

CREATE INDEX idx_timeline_event_links_b ON timeline_event_links (event_b_id);

CREATE TABLE timeline_event_sources (
    id         BIGSERIAL PRIMARY KEY,
    event_id   BIGINT NOT NULL REFERENCES timeline_events(id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    author     TEXT NOT NULL DEFAULT '',
    citation   TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0
);

CREATE INDEX idx_timeline_event_sources_event ON timeline_event_sources (event_id);

-- +goose Down

DROP TABLE IF EXISTS timeline_event_sources;
DROP TABLE IF EXISTS timeline_event_links;
DROP TABLE IF EXISTS timeline_event_people;
DROP TABLE IF EXISTS timeline_people;
DROP TABLE IF EXISTS timeline_event_tags;
DROP TABLE IF EXISTS timeline_events;
DROP TABLE IF EXISTS timeline_tags;
DROP TABLE IF EXISTS timeline_categories;
DROP TABLE IF EXISTS timeline_timelines;
