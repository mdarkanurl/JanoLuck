-- +goose up

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    email       TEXT            NOT NULL    UNIQUE,
    password    TEXT            NOT NULL,
    create_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    update_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

-- +goose down
DROP TABLE users;
