-- +goose Up

CREATE TABLE timelines (
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

CREATE TABLE categories (
    id          BIGSERIAL PRIMARY KEY,
    timeline_id BIGINT NOT NULL REFERENCES timelines(id) ON DELETE CASCADE,
    slug        TEXT NOT NULL,
    name        TEXT NOT NULL,
    color       TEXT NOT NULL DEFAULT '#6366f1',
    sort_order  INT NOT NULL DEFAULT 0,
    UNIQUE(timeline_id, slug)
);

CREATE INDEX idx_categories_timeline ON categories (timeline_id);

CREATE TABLE tags (
    id   BIGSERIAL PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

CREATE TABLE events (
    id             BIGSERIAL PRIMARY KEY,
    timeline_id    BIGINT NOT NULL REFERENCES timelines(id) ON DELETE CASCADE,
    slug           TEXT NOT NULL,
    title          TEXT NOT NULL,
    summary        TEXT NOT NULL DEFAULT '',
    article        TEXT NOT NULL DEFAULT '',
    event_date     DATE NOT NULL,
    event_end_date DATE,
    date_precision TEXT NOT NULL DEFAULT 'day',
    sort_order     INT NOT NULL DEFAULT 0,
    category_id    BIGINT REFERENCES categories(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(timeline_id, slug)
);

CREATE INDEX idx_events_timeline_date ON events (timeline_id, event_date, sort_order);
CREATE INDEX idx_events_timeline_category_date ON events (timeline_id, category_id, event_date);
CREATE INDEX idx_events_search ON events USING gin (
    to_tsvector('english', title || ' ' || summary || ' ' || article)
);

CREATE TABLE event_tags (
    event_id BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    tag_id   BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (event_id, tag_id)
);

CREATE INDEX idx_event_tags_tag ON event_tags (tag_id);

-- +goose Down
DROP TABLE IF EXISTS event_tags;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS timelines;
