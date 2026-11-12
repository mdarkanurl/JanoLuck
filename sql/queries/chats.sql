-- name: CreateChat :one
INSERT INTO chats (sessionId, userId, role, content, embedding)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, sessionId, userId, role, content, create_at, update_at, moderation_status, moderation_reason;

-- name: SessionExistsByUserID :one
SELECT EXISTS (
    SELECT 1
    FROM sessions
    WHERE id = $1 AND userId = $2
);

-- name: IsItFirstChat :one
SELECT EXISTS(
    SELECT 1
    FROM chats
    WHERE sessionId = $1
);

-- name: ListChatsBySessionID :many
SELECT id, sessionId, userId, role, content, create_at, update_at, moderation_status, moderation_reason
FROM chats
WHERE sessionId = $1 AND userId = $2
ORDER BY create_at DESC, id DESC
LIMIT $3 OFFSET $4;

-- name: ListSimilarChatsBySessionID :many
SELECT content
FROM chats
WHERE sessionId = sqlc.arg(sessionID)
  AND id <> sqlc.arg(excludeChatID)
  AND embedding IS NOT NULL
ORDER BY embedding <=> sqlc.arg(embedding)::text::vector
LIMIT sqlc.arg(rowLimit);

-- name: CountChatsBySessionID :one
SELECT COUNT(*)
FROM chats
WHERE sessionId = $1 AND userId = $2;

-- name: GetChatByID :one
SELECT id, sessionId, userId, role, content, create_at, update_at, moderation_status, moderation_reason
FROM chats
WHERE id = $1 AND userId = $2;

-- name: UpdateChatContent :one
UPDATE chats
SET content = $3, update_at = $4
WHERE id = $1 AND userId = $2
RETURNING id, sessionId, userId, role, content, create_at, update_at, moderation_status, moderation_reason;

-- name: UpdateChatEmbedding :execrows
UPDATE chats
SET embedding = $2, update_at = $3
WHERE id = $1;

-- name: UpdateChatModeration :execrows
UPDATE chats
SET moderation_status = $2, moderation_reason = $3, update_at = $4
WHERE id = $1;

-- name: ListChatsForEmbedding :many
SELECT id, sessionId, userId, content
FROM chats
WHERE (sqlc.arg(includeAll)::boolean OR embedding IS NULL)
ORDER BY create_at ASC;

-- name: ListRecentChatContentsBySessionID :many
SELECT content
FROM chats
WHERE sessionId = $1
ORDER BY create_at DESC, id DESC
LIMIT $2;

-- name: DeleteChat :execrows
DELETE FROM chats
WHERE id = $1 AND userId = $2;
