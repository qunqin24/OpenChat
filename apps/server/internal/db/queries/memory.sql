-- name: GetPersonalization :one
SELECT instructions_about, instructions_style, memory_enabled
FROM users
WHERE id = $1;

-- name: UpdatePersonalization :one
-- Its own statement, apart from UpdateOwnProfile, for the same reason that one
-- is apart from AdminUpdateUser: nothing here reaches the role or the password.
UPDATE users
SET instructions_about = COALESCE(sqlc.narg('instructions_about')::text, instructions_about),
    instructions_style = COALESCE(sqlc.narg('instructions_style')::text, instructions_style),
    memory_enabled     = COALESCE(sqlc.narg('memory_enabled')::boolean, memory_enabled),
    updated_at         = now()
WHERE id = sqlc.arg('id')
RETURNING instructions_about, instructions_style, memory_enabled;

-- name: ListMemories :many
-- Oldest first, so a memory keeps its place in the list (and in the prompt).
SELECT *
FROM user_memories
WHERE user_id = $1
ORDER BY created_at, id;

-- name: CountMemories :one
SELECT count(*)
FROM user_memories
WHERE user_id = $1;

-- name: GetMemory :one
SELECT *
FROM user_memories
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id');

-- name: LockMemoryList :exec
-- Serialises writers of one person's list for the length of the transaction, so
-- two adds at the cap cannot both pass the count.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('user_id')::text, 7));

-- name: InsertMemory :one
INSERT INTO user_memories (id, user_id, content, source, conversation_id)
VALUES (COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()), sqlc.arg('user_id'), sqlc.arg('content'),
        sqlc.arg('source'), sqlc.narg('conversation_id'))
RETURNING *;

-- name: UpdateMemoryContent :one
UPDATE user_memories
SET content    = sqlc.arg('content'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
RETURNING *;

-- name: DeleteMemory :execrows
DELETE FROM user_memories
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id');

-- name: DeleteAllMemories :exec
DELETE FROM user_memories
WHERE user_id = $1;

-- name: InsertMemoryEvent :one
INSERT INTO memory_events (user_id, memory_id, op, before, after, source, conversation_id)
VALUES (sqlc.arg('user_id'), sqlc.arg('memory_id'), sqlc.arg('op'), sqlc.narg('before'), sqlc.narg('after'),
        sqlc.arg('source'), sqlc.narg('conversation_id'))
RETURNING *;

-- name: ListMemoryEvents :many
SELECT *
FROM memory_events
WHERE user_id = sqlc.arg('user_id')
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: GetMemoryEventForUpdate :one
SELECT *
FROM memory_events
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
FOR UPDATE;

-- name: MarkMemoryEventUndone :one
UPDATE memory_events
SET undone_at = now()
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
RETURNING *;

-- name: DeleteMemoryEventsFor :exec
-- What the log holds about a memory goes when the person deletes the memory.
DELETE FROM memory_events
WHERE user_id = sqlc.arg('user_id')
  AND memory_id = sqlc.arg('memory_id');

-- name: DeleteAllMemoryEvents :exec
DELETE FROM memory_events
WHERE user_id = $1;

-- name: DeleteOldMemoryEvents :execrows
DELETE FROM memory_events
WHERE created_at < sqlc.arg('before');

-- name: GetConversationSnapshotForUpdate :one
-- The snapshot, with the row locked: the tool adds to it as memories are saved.
SELECT memory_snapshot, temporary
FROM conversations
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
FOR UPDATE;

-- name: GetConversationMemoryState :one
-- Whether the conversation exists for this user, is temporary, and its snapshot.
SELECT memory_snapshot, temporary
FROM conversations
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id');

-- name: SetConversationSnapshot :one
-- Only the first one is kept: two requests that both found none cannot
-- overwrite each other, and the loser reads the winner's.
UPDATE conversations
SET memory_snapshot = sqlc.arg('snapshot')::jsonb
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
  AND memory_snapshot IS NULL
RETURNING memory_snapshot;

-- name: WriteConversationSnapshot :exec
UPDATE conversations
SET memory_snapshot = sqlc.arg('snapshot')::jsonb
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id');

-- name: DeleteExpiredTemporaryConversations :execrows
-- One statement, so any number of servers can run it: whichever gets a row
-- deletes it, the others find it gone. Soft-deleted ones go too — a day is a day.
-- Messages, runs and attachments follow by cascade; usage rows are kept (their
-- conversation reference is SET NULL).
DELETE FROM conversations
WHERE temporary
  AND created_at < sqlc.arg('before');
