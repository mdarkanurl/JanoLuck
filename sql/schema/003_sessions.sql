-- +goose up

CREATE TABLE sessions (
    id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT            NOT NULL,
    userId      UUID            NOT NULL,
    create_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    update_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_sessions_users
        FOREIGN KEY (userId) REFERENCES users(id)
        ON DELETE CASCADE
);

-- +goose down
DROP TABLE sessions;
