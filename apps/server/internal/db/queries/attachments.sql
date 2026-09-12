-- name: CreateAttachment :one
INSERT INTO attachments (message_id, storage_key, filename, content_type, size_bytes)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: AttachToMessage :exec
UPDATE attachments
SET message_id = sqlc.arg('message_id')
WHERE id = ANY (sqlc.arg('ids')::uuid[]);

-- name: ListAttachmentsByMessages :many
SELECT *
FROM attachments
WHERE message_id = ANY (sqlc.arg('message_ids')::uuid[])
ORDER BY created_at;
