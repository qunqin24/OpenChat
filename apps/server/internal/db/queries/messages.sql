-- name: CreateMessage :one
INSERT INTO messages (conversation_id, role, content, status)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: AppendMessageContent :one
UPDATE messages
SET content    = content || sqlc.arg('delta')::text,
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: FinishMessage :one
UPDATE messages
SET content    = sqlc.arg('content'),
    status     = sqlc.arg('status'),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ListMessages :many
SELECT *
FROM messages
WHERE conversation_id = sqlc.arg('conversation_id')
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, id) > (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY created_at, id
LIMIT sqlc.arg('row_limit');
