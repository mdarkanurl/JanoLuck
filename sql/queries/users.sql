-- name: CreateUser :exec
INSERT INTO users (email, password, update_at)
VALUES ($1, $2, $3);

-- name: UserExistsByEmail :one
SELECT EXISTS (
    SELECT 1
    FROM users
    WHERE email = $1
);
