-- +goose up

CREATE TABLE audit_log (
    id          BIGSERIAL       PRIMARY KEY,
    event_type  TEXT            NOT NULL,
    userId      UUID,
    email       TEXT,
    metadata    JSONB           NOT NULL DEFAULT '{}'::jsonb,
    create_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX audit_log_event_type_idx ON audit_log (event_type, create_at DESC);
CREATE INDEX audit_log_user_idx ON audit_log (userId, create_at DESC) WHERE userId IS NOT NULL;

-- +goose down
DROP TABLE audit_log;
