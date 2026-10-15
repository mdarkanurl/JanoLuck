-- name: CreateProfile :one
INSERT INTO profiles (name, questions, userId)
VALUES ($1, $2, $3)
RETURNING id, name, questions, userId;

-- name: GetProfileByUserID :one
SELECT id, name, questions, userId
FROM profiles
WHERE userId = $1;
