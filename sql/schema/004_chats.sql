-- +goose up

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE chats (
    id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    sessionId  UUID,
    userId     UUID,
    role       TEXT CHECK (role IN ('user', 'system')),
    content     text NOT NULL,
    embedding   vector(1536),
    create_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    update_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_chats_users 
        FOREIGN KEY (userId) REFERENCES users(id)
        ON DELETE CASCADE,

    CONSTRAINT fk_chats_sessions 
        FOREIGN KEY (sessionId) REFERENCES sessions(id)
        ON DELETE CASCADE
);

CREATE INDEX ON chats USING hnsw (embedding vector_cosine_ops);

-- +goose down
DROP TABLE chats;