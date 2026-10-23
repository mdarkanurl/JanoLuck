CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE chats (
    id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    sessionId  UUID,
    userId     UUID,
    role        'user' | 'system',
    content     text NOT NULL,
    embedding   vector(1536),
    metadata    jsonb DEFAULT '{}',
    create_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    update_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    FOREIGN KEY (userId) REFERENCES users(id)
);

CREATE INDEX ON chats 
  USING hnsw (embedding vector_cosine_ops)
  WITH (m = 16, ef_construction = 64);

CREATE INDEX ON chats (sessionId, created_at);
CREATE INDEX ON chats USING gin (metadata);
