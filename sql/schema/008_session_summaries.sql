-- +goose up

CREATE TABLE session_summaries (
    sessionId       UUID            PRIMARY KEY,
    summary         TEXT            NOT NULL,
    coveredChats    INTEGER         NOT NULL DEFAULT 0,
    create_at       TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    update_at       TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_session_summaries_sessions
        FOREIGN KEY (sessionId) REFERENCES sessions(id)
        ON DELETE CASCADE
);

-- +goose down
DROP TABLE session_summaries;
