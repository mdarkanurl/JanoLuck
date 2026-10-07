-- +goose up

CREATE TABLE users (
    id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    gmail       TEXT            NOT NULL    UNIQUE,
    password    VARCHAR(128)    NOT NULL,
    create_at   TIMESTAMP       NOT NULL DEFAULT CURRENT_DATE,
    update_at   TIMESTAMP       NOT NULL
);

-- +goose down
DROP TABLE users;
