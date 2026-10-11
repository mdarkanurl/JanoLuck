-- name: CreateUser :one
INSERT INTO users (email, password, update_at)
VALUES ($1, $2, $3)
RETURNING id, email;

-- name: GetUserByEmail :one
SELECT id, email, create_at, update_at
FROM users
WHERE email = $1;

-- name: GetUserByID :one
SELECT id, email, create_at, update_at
FROM users
WHERE id = $1;

-- name: UserExistsByEmail :one
SELECT EXISTS (
    SELECT 1
    FROM users
    WHERE email = $1
);
