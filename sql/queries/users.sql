-- name: CreateUser :one
INSERT INTO users (gmail, password, update_at)
VALUES ($1, $2, $3)
RETURNING id, gmail;

-- name: GetUserByGmail :one
SELECT id, gmail, password, create_at, update_at
FROM users
WHERE gmail = $1;
