-- name: EnqueueOutbox :one
INSERT INTO outbox (topic, key, payload, headers)
VALUES ($1, $2, $3, $4)
RETURNING id, topic, key, payload, headers, create_at, published_at;

-- name: ListPendingOutbox :many
SELECT id, topic, key, payload, headers, create_at
FROM outbox
WHERE published_at IS NULL
ORDER BY id
LIMIT $1;

-- name: MarkOutboxPublished :execrows
UPDATE outbox
SET published_at = NOW()
WHERE id = $1 AND published_at IS NULL;
