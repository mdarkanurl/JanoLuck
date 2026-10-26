-- name: CreateChat :one
INSERT INTO chats (sessionId, userId, role, content)
VALUES ($1, $2, $3, $4)
RETURNING id, sessionId, userId, role, content, create_at, update_at;

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
SELECT id, sessionId, userId, role, content, create_at, update_at
FROM chats
WHERE sessionId = $1 AND userId = $2
ORDER BY create_at ASC, id ASC;

-- name: GetChatByID :one
SELECT id, sessionId, userId, role, content, create_at, update_at
FROM chats
WHERE id = $1 AND userId = $2;

-- name: UpdateChatContent :one
UPDATE chats
SET content = $3, update_at = $4
WHERE id = $1 AND userId = $2
RETURNING id, sessionId, userId, role, content, create_at, update_at;

-- name: DeleteChat :execrows
DELETE FROM chats
WHERE id = $1 AND userId = $2;
