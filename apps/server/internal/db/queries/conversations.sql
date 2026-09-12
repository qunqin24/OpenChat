-- name: CreateConversation :one
INSERT INTO conversations (user_id, title, model)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetConversation :one
-- Ownership is part of the lookup, not a separate check: a conversation
-- belonging to someone else must read as "not found", never as "forbidden",
-- or the response tells a stranger the id exists.
SELECT *
FROM conversations
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL;

-- name: ListConversations :many
SELECT *
FROM conversations
WHERE user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL
  AND (sqlc.narg('cursor_updated_at')::timestamptz IS NULL
    OR (updated_at, id) < (sqlc.narg('cursor_updated_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY updated_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: UpdateConversationTitle :one
UPDATE conversations
SET title      = sqlc.arg('title'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL
RETURNING *;

-- name: TouchConversation :exec
UPDATE conversations
SET updated_at = now()
WHERE id = $1;

-- name: SoftDeleteConversation :execrows
UPDATE conversations
SET deleted_at = now()
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL;
