-- name: CreateSession :one
INSERT INTO sessions (name, userId, update_at)
VALUES ($1, $2, $3)
RETURNING id, name, userId, create_at, update_at;

-- name: ListSessionsByUserID :many
SELECT id, name, userId, create_at, update_at
FROM sessions
WHERE userId = $1
ORDER BY update_at DESC, create_at DESC
LIMIT $2 OFFSET $3;

-- name: CountSessionsByUserID :one
SELECT COUNT(*)
FROM sessions
WHERE userId = $1;

-- name: GetSessionByID :one
SELECT id, name, userId, create_at, update_at
FROM sessions
WHERE id = $1 AND userId = $2;

-- name: DeleteSession :execrows
DELETE FROM sessions
WHERE id = $1 AND userId = $2;
