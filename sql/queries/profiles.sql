-- name: CreateProfile :one
INSERT INTO profiles (name, questions, userId)
VALUES ($1, $2, $3)
RETURNING id, name, questions, userId;

-- name: GetProfileByUserID :one
SELECT id, name, questions, userId
FROM profiles
WHERE userId = $1;

-- name: UpdateProfile :one
UPDATE profiles
SET name = $2, questions = $3
WHERE userId = $1
RETURNING id, name, questions, userId;

-- name: ProfileExistsByUserID :one
SELECT EXISTS(
    SELECT 1
    FROM profiles
    WHERE userId = $1
);
