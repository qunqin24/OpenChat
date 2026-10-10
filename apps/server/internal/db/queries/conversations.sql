-- name: CreateConversation :one
INSERT INTO conversations (user_id, title, model, temporary)
VALUES ($1, $2, $3, $4)
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
  -- A temporary conversation is not history: it is reached by its link, once.
  AND NOT temporary
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

-- name: SoftDeleteAllConversations :execrows
-- Everything the user has, temporary chats included, in one statement. The
-- owner is the only filter there is: nobody else's rows can be reached from here.
UPDATE conversations
SET deleted_at = now()
WHERE user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL;

-- name: SetConversationModel :one
-- The model a conversation's next run uses. Remembered per conversation so a
-- reload, or another device, continues with the same one.
UPDATE conversations
SET model = sqlc.arg('model')
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL
RETURNING *;

-- name: SetConversationReasoning :one
-- NULL is automatic: the model's own default.
UPDATE conversations
SET reasoning_effort = sqlc.narg('reasoning_effort')
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL
RETURNING *;

-- name: SetConversationWebSearch :one
UPDATE conversations
SET web_search = sqlc.arg('web_search')
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL
RETURNING *;

-- name: SetGeneratedTitle :one
-- A generated title, only over the default one: a conversation the user has
-- renamed in the meantime keeps their name. Not a change of activity, so the
-- conversation keeps its place in the list.
UPDATE conversations
SET title = sqlc.arg('title')
WHERE id = sqlc.arg('id')
  AND title = sqlc.arg('default_title')
  AND deleted_at IS NULL
RETURNING *;

-- name: LockConversation :one
-- Held until the transaction ends: whatever moves the conversation's current
-- leaf — a message, an edit, a switch of branch — queues here, and reads the
-- leaf the one before it left.
SELECT *
FROM conversations
WHERE id = $1
FOR UPDATE;

-- name: SetCurrentInput :exec
-- Moves the reader to this user message, a leaf of the conversation, and notes
-- that it was the branch shown just now (see PickBranchLeaf).
WITH shown AS (
    UPDATE messages
        SET shown_at = clock_timestamp()
        WHERE messages.id = sqlc.arg('input_id')
            AND messages.conversation_id = sqlc.arg('conversation_id'))
UPDATE conversations
SET current_input_id = sqlc.arg('input_id')
WHERE conversations.id = sqlc.arg('conversation_id');
