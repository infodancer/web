-- +goose Up

CREATE TABLE images (
    id           BIGSERIAL PRIMARY KEY,
    timeline_id  BIGINT NOT NULL REFERENCES timelines(id) ON DELETE CASCADE,
    slug         TEXT NOT NULL,
    usage        TEXT NOT NULL DEFAULT 'general',
    content_type TEXT NOT NULL,
    width        INT NOT NULL,
    height       INT NOT NULL,
    file_size    INT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(timeline_id, slug)
);

CREATE INDEX idx_images_timeline ON images(timeline_id);
CREATE INDEX idx_images_usage ON images(timeline_id, usage);

-- +goose Down

DROP TABLE IF EXISTS images;
