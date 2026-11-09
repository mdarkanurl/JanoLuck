-- name: CreateMode :one
INSERT INTO modes (userId, name, prompt)
VALUES ($1, $2, $3)
RETURNING id, userId, name, prompt, isBuiltIn, create_at, update_at;

-- name: ListModesByUserID :many
SELECT id, userId, name, prompt, isBuiltIn, create_at, update_at
FROM modes
WHERE userId = $1 OR isBuiltIn = TRUE
ORDER BY isBuiltIn DESC, lower(name) ASC
LIMIT $2 OFFSET $3;

-- name: CountModesByUserID :one
SELECT COUNT(*)
FROM modes
WHERE userId = $1 OR isBuiltIn = TRUE;

-- name: GetModeByID :one
SELECT id, userId, name, prompt, isBuiltIn, create_at, update_at
FROM modes
WHERE id = $1 AND (userId = $2 OR isBuiltIn = TRUE);

-- name: UpdateMode :one
UPDATE modes
SET name = $3, prompt = $4, update_at = $5
WHERE id = $1 AND userId = $2 AND isBuiltIn = FALSE
RETURNING id, userId, name, prompt, isBuiltIn, create_at, update_at;

-- name: DeleteMode :execrows
DELETE FROM modes
WHERE id = $1 AND userId = $2 AND isBuiltIn = FALSE;

-- name: GetActiveModeByUserID :one
SELECT m.id, m.userId, m.name, m.prompt, m.isBuiltIn, m.create_at, m.update_at
FROM user_active_modes uam
JOIN modes m ON m.id = uam.modeId
WHERE uam.userId = $1;

-- name: SetActiveMode :one
INSERT INTO user_active_modes (userId, modeId, update_at)
SELECT sqlc.arg(userID), sqlc.arg(modeID), sqlc.arg(updateAt)
FROM modes
WHERE id = sqlc.arg(modeID)
  AND (userId = sqlc.arg(userID) OR isBuiltIn = TRUE)
ON CONFLICT (userId) DO UPDATE
SET modeId = EXCLUDED.modeId, update_at = EXCLUDED.update_at
RETURNING modeId;

-- name: ClearActiveMode :execrows
DELETE FROM user_active_modes
WHERE userId = $1;
