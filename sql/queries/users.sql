-- name: CreateUser :one
INSERT INTO users (gmail, password, update_at)
VALUES ($1, $2, $3)
RETURNING id, gmail;
