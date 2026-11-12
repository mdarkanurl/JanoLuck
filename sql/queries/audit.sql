-- name: CreateAuditLog :exec
INSERT INTO audit_log (event_type, userId, email, metadata)
VALUES ($1, $2, $3, $4);
