-- +goose up

CREATE TABLE outbox (
    id              BIGSERIAL       PRIMARY KEY,
    topic           TEXT            NOT NULL,
    key             TEXT            NOT NULL DEFAULT '',
    payload         JSONB           NOT NULL,
    headers         JSONB           NOT NULL DEFAULT '{}'::jsonb,
    create_at       TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    published_at    TIMESTAMPTZ
);

CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;

-- +goose down
DROP TABLE outbox;
