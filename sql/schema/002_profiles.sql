-- +goose up

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE profiles (
    id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT            NOT NULL,
    questions   JSONB           NOT NULL,
    userId      UUID            NOT NULL UNIQUE,
    create_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    update_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    FOREIGN KEY (userId) REFERENCES users(id)
);

-- +goose down
DROP TABLE profiles;
