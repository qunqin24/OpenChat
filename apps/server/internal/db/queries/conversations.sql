-- name: CreateConversation :one
INSERT INTO conversations (user_id, title, model)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetConversation :one
SELECT *
FROM conversations
WHERE id = $1
  AND deleted_at IS NULL;

-- name: ListConversations :many
SELECT *
FROM conversations
WHERE deleted_at IS NULL
  AND (sqlc.narg('cursor_updated_at')::timestamptz IS NULL
    OR (updated_at, id) < (sqlc.narg('cursor_updated_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY updated_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: UpdateConversationTitle :one
UPDATE conversations
SET title      = $2,
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
RETURNING *;

-- name: TouchConversation :exec
UPDATE conversations
SET updated_at = now()
WHERE id = $1;

-- name: SoftDeleteConversation :execrows
UPDATE conversations
SET deleted_at = now()
WHERE id = $1
  AND deleted_at IS NULL;
