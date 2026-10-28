-- name: CreateConversation :one
INSERT INTO conversations (user_id, title, model, temporary)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetConversation :one
-- Ownership is part of the lookup, not a separate check: a conversation
-- belonging to someone else must read as "not found", never as "forbidden",
-- or the response tells a stranger the id exists.
-- A sub-agent's conversation (kind 'subagent') is not one either: nothing a
-- person sends reaches it, so no endpoint that takes their words finds it.
SELECT *
FROM conversations
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
  AND kind = 'chat'
  AND deleted_at IS NULL;

-- name: ListConversations :many
SELECT *
FROM conversations
WHERE user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL
  AND kind = 'chat'
  -- A temporary conversation is not history: it is reached by its link, once.
  AND NOT temporary
  AND (sqlc.narg('cursor_updated_at')::timestamptz IS NULL
    OR (updated_at, id) < (sqlc.narg('cursor_updated_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY updated_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: SearchConversations :many
-- The caller's conversations whose title or any message's text contains the
-- pattern (an ILIKE pattern, escaped by the caller), newest first, each with
-- the latest message that matched. Reasoning and tool rows are not words
-- the person wrote or read as the answer, so only user and assistant text -
-- and not a message moderation refused, which was never sent.
-- A conversation found by its title alone has no hit: empty strings, not NULLs.
SELECT c.*,
       COALESCE(hit.id::text, '')::text  AS hit_id,
       COALESCE(hit.role::text, '')::text AS hit_role,
       COALESCE(hit.content, '')::text    AS hit_content
FROM conversations c
         LEFT JOIN LATERAL (
    SELECT m.id, m.role, m.content
    FROM messages m
    WHERE m.conversation_id = c.id
      AND m.role IN ('user', 'assistant')
      AND m.status <> 'blocked'
      AND m.origin IS NULL
      AND m.content ILIKE sqlc.arg('pattern')::text
    ORDER BY m.created_at DESC, m.id DESC
    LIMIT 1
    ) hit ON true
WHERE c.user_id = sqlc.arg('user_id')
  AND c.deleted_at IS NULL
  AND c.kind = 'chat'
  AND NOT c.temporary
  AND (c.title ILIKE sqlc.arg('pattern')::text OR hit.id IS NOT NULL)
  AND (sqlc.narg('cursor_updated_at')::timestamptz IS NULL
    OR (c.updated_at, c.id) < (sqlc.narg('cursor_updated_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY c.updated_at DESC, c.id DESC
LIMIT sqlc.arg('row_limit');

-- name: ListRunningConversations :many
-- The caller's listed conversations with a run in progress; at most one run
-- per conversation is (runs_one_active_per_conversation), so no duplicates.
SELECT r.conversation_id
FROM runs r
         JOIN conversations c ON c.id = r.conversation_id
WHERE r.status = 'running'
  AND c.user_id = sqlc.arg('user_id')
  AND c.deleted_at IS NULL
  AND c.kind = 'chat'
  AND NOT c.temporary;

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

-- name: LockUserConversations :many
-- Every live conversation of the user, held until the transaction ends, in id
-- order. Deleting them all takes the same per-conversation lock a send, an
-- edit or a regeneration does (LockConversation), so none of those can be
-- starting a run in one of them while it goes.
SELECT id
FROM conversations
WHERE user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL
ORDER BY id
FOR UPDATE;

-- name: CancelConversationRuns :many
-- Stop pressed for every run these conversations have going, as the stop
-- endpoint does for one: recorded here, and the runner told after the commit.
-- Deleting a conversation does this, because nothing can stop a run in a
-- deleted conversation afterwards (the stop endpoint no longer finds it) and
-- it would go on calling the model and holding its owner's run slot. The
-- runs of their sub-agents go too.
UPDATE runs
SET cancel_requested = true,
    updated_at       = now()
WHERE (conversation_id = ANY (sqlc.arg('ids')::uuid[])
    OR conversation_id IN (SELECT c.id FROM conversations c WHERE c.parent_id = ANY (sqlc.arg('ids')::uuid[])))
  AND status = 'running'
RETURNING id;

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
-- Held until the transaction ends: whatever changes what the conversation
-- holds - a message, an edit, a regeneration - queues here, and reads the
-- conversation as the one before it left it.
SELECT *
FROM conversations
WHERE id = $1
FOR UPDATE;

-- name: ListPinnedConversations :many
-- The caller's pinned conversations, the latest pinned first: shown above the
-- rest of the list whatever their age, so not paged with it.
SELECT *
FROM conversations
WHERE user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL
  AND kind = 'chat'
  AND NOT temporary
  AND pinned_at IS NOT NULL
ORDER BY pinned_at DESC, id DESC
LIMIT 200;

-- name: SetConversationPinned :one
-- Pins or unpins without touching updated_at: pinning is not activity.
UPDATE conversations
SET pinned_at = CASE WHEN sqlc.arg('pinned')::boolean THEN COALESCE(pinned_at, now()) ELSE NULL END
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
  AND deleted_at IS NULL
RETURNING *;
