-- name: GetPersonalization :one
SELECT instructions_about,
       instructions_style,
       memory_enabled,
       chat_history,
       instructions_about_checked_hash,
       instructions_about_blocked_hash,
       instructions_style_checked_hash,
       instructions_style_blocked_hash
FROM users
WHERE id = $1;

-- name: UpdatePersonalization :one
-- Its own statement, apart from UpdateOwnProfile, for the same reason that one
-- is apart from AdminUpdateUser: nothing here reaches the role or the password.
--
-- A field that changes takes the hash of the text that was moderated (NULL when
-- it was not, or could not be, checked) and forgets the hash of what was
-- refused; a field sent unchanged, or not sent, keeps both.
UPDATE users
SET instructions_about = COALESCE(sqlc.narg('instructions_about')::text, instructions_about),
    instructions_style = COALESCE(sqlc.narg('instructions_style')::text, instructions_style),
    memory_enabled     = COALESCE(sqlc.narg('memory_enabled')::boolean, memory_enabled),
    chat_history       = COALESCE(sqlc.narg('chat_history')::boolean, chat_history),
    instructions_about_checked_hash = CASE
        WHEN sqlc.narg('instructions_about')::text IS NULL OR sqlc.narg('instructions_about')::text = instructions_about
            THEN instructions_about_checked_hash
        ELSE sqlc.narg('about_checked_hash')::text END,
    instructions_about_blocked_hash = CASE
        WHEN sqlc.narg('instructions_about')::text IS NULL OR sqlc.narg('instructions_about')::text = instructions_about
            THEN instructions_about_blocked_hash
        ELSE NULL END,
    instructions_style_checked_hash = CASE
        WHEN sqlc.narg('instructions_style')::text IS NULL OR sqlc.narg('instructions_style')::text = instructions_style
            THEN instructions_style_checked_hash
        ELSE sqlc.narg('style_checked_hash')::text END,
    instructions_style_blocked_hash = CASE
        WHEN sqlc.narg('instructions_style')::text IS NULL OR sqlc.narg('instructions_style')::text = instructions_style
            THEN instructions_style_blocked_hash
        ELSE NULL END,
    updated_at         = now()
WHERE id = sqlc.arg('id')
RETURNING instructions_about, instructions_style, memory_enabled, chat_history;

-- name: MarkInstructionsModerated :exec
-- What a snapshot-time check decided about the instructions, recorded only
-- while the text still hashes to what was checked: an edit made meanwhile is
-- another text, and nobody has looked at it. A NULL argument leaves its column.
UPDATE users
SET instructions_about_checked_hash = COALESCE(
        CASE WHEN encode(sha256(convert_to(instructions_about, 'UTF8')), 'hex') = sqlc.narg('about_checked')::text
                 THEN sqlc.narg('about_checked')::text END,
        instructions_about_checked_hash),
    instructions_about_blocked_hash = COALESCE(
        CASE WHEN encode(sha256(convert_to(instructions_about, 'UTF8')), 'hex') = sqlc.narg('about_blocked')::text
                 THEN sqlc.narg('about_blocked')::text END,
        instructions_about_blocked_hash),
    instructions_style_checked_hash = COALESCE(
        CASE WHEN encode(sha256(convert_to(instructions_style, 'UTF8')), 'hex') = sqlc.narg('style_checked')::text
                 THEN sqlc.narg('style_checked')::text END,
        instructions_style_checked_hash),
    instructions_style_blocked_hash = COALESCE(
        CASE WHEN encode(sha256(convert_to(instructions_style, 'UTF8')), 'hex') = sqlc.narg('style_blocked')::text
                 THEN sqlc.narg('style_blocked')::text END,
        instructions_style_blocked_hash)
WHERE id = sqlc.arg('id');

-- name: ListMemories :many
-- Oldest first, so a memory keeps its place in the list (and in the prompt).
SELECT *
FROM user_memories
WHERE user_id = $1
ORDER BY created_at, id;

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
INSERT INTO user_memories (id, user_id, content, source, conversation_id, checked_hash)
VALUES (COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()), sqlc.arg('user_id'), sqlc.arg('content'),
        sqlc.arg('source'), sqlc.narg('conversation_id'), sqlc.narg('checked_hash'))
RETURNING *;

-- name: UpdateMemoryContent :one
-- The new text is unchecked (NULL hash) unless the caller vouches for it, and
-- nothing is known to have been refused about it.
UPDATE user_memories
SET content      = sqlc.arg('content'),
    checked_hash = sqlc.narg('checked_hash'),
    blocked_hash = NULL,
    updated_at   = now()
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
RETURNING *;

-- name: MarkMemoriesModerated :exec
-- What a snapshot-time check decided about memories: `ok` or `blocked`, each
-- with the hash of the text that was checked, recorded only where the memory
-- still hashes to it. updated_at is left alone - this is not an edit.
UPDATE user_memories m
SET checked_hash = CASE WHEN d.verdict = 'ok' THEN h.hash ELSE m.checked_hash END,
    blocked_hash = CASE WHEN d.verdict = 'blocked' THEN h.hash ELSE m.blocked_hash END
FROM unnest(sqlc.arg('ids')::uuid[]) WITH ORDINALITY AS i (id, n)
         JOIN unnest(sqlc.arg('hashes')::text[]) WITH ORDINALITY AS h (hash, n) USING (n)
         JOIN unnest(sqlc.arg('verdicts')::text[]) WITH ORDINALITY AS d (verdict, n) USING (n)
WHERE m.user_id = sqlc.arg('user_id')
  AND m.id = i.id
  AND encode(sha256(convert_to(m.content, 'UTF8')), 'hex') = h.hash;

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

-- name: ListRecentChatCandidates :many
-- The person's recent conversations for the list in a new conversation's
-- snapshot (docs/design/chat-history-plan.md 3.1), each with the user messages
-- it would show: the first three and the last ten of the ones that count, of
-- which the caller keeps the first and the last five that still have words in
-- them once the composer's lines are taken off. Their own conversations only
-- (user_id is in the statement): chats - not a sub-agent's, a temporary one, a
-- scheduled task's (that is the task's instruction, not what they typed), a
-- deleted one or the current one - with at least one user message that
-- moderation did not refuse and that nothing but the person wrote
-- (origin IS NULL).
--
-- Which conversations is decided by their last activity (updated_at, the order
-- of the sidebar, read off conversations_user_updated_idx), so that only the
-- messages of the ones listed are read, however long the person's history is;
-- among those, the newest last user message comes first. The content is cut
-- at 20000 characters: more than any quote plus the request after it that
-- matters, and no transcript is loaded whole.
WITH chats AS (
    SELECT c.id, c.title, c.title_checked_hash, c.title_blocked_hash
    FROM conversations c
    WHERE c.user_id = sqlc.arg('user_id')
      AND c.deleted_at IS NULL
      AND c.kind = 'chat'
      AND NOT c.temporary
      AND c.id <> sqlc.arg('current_id')
      AND NOT EXISTS (SELECT 1 FROM scheduled_tasks t WHERE t.conversation_id = c.id)
      AND EXISTS (SELECT 1
                  FROM messages m
                  WHERE m.conversation_id = c.id
                    AND m.role = 'user'
                    AND m.status <> 'blocked'
                    AND m.origin IS NULL)
    ORDER BY c.updated_at DESC, c.id DESC
    LIMIT sqlc.arg('row_limit')
),
     numbered AS (
         SELECT m.conversation_id, m.created_at, left(m.content, 20000) AS content,
                row_number() OVER (PARTITION BY m.conversation_id ORDER BY m.created_at, m.id)           AS first_n,
                row_number() OVER (PARTITION BY m.conversation_id ORDER BY m.created_at DESC, m.id DESC) AS last_n,
                (max(m.created_at) OVER (PARTITION BY m.conversation_id))::timestamptz                    AS last_at
         FROM messages m
         WHERE m.conversation_id IN (SELECT id FROM chats)
           AND m.role = 'user'
           AND m.status <> 'blocked'
           AND m.origin IS NULL
     )
SELECT chats.id, chats.title, chats.title_checked_hash, chats.title_blocked_hash, numbered.last_at,
       numbered.created_at AS message_at, numbered.content
FROM chats
         JOIN numbered ON numbered.conversation_id = chats.id
WHERE numbered.first_n <= 3
   OR numbered.last_n <= 10
ORDER BY numbered.last_at DESC, chats.id DESC, numbered.created_at, numbered.first_n;

-- name: LockRecentChats :exec
-- Serialises, per person, the two things that must not interleave: deleting
-- conversations (which takes them out of the lists in the person's other
-- conversations) and saving a new list. Without it two deletions each hold
-- their own conversation's row and wait for the other's, and a list taken just
-- before a deletion could be saved just after it, with the deleted conversation
-- still in it. Taken first in its transaction, before any conversation row.
SELECT pg_advisory_xact_lock(hashtextextended('recent_chats:' || sqlc.arg('user_id')::uuid::text, 0));

-- name: ListLiveConversationIDs :many
-- Which of these conversations of the person still exist: a list about to be
-- saved drops the ones deleted since it was read.
SELECT id
FROM conversations
WHERE user_id = sqlc.arg('user_id')
  AND id = ANY (sqlc.arg('ids')::uuid[])
  AND deleted_at IS NULL;

-- name: MarkTitlesModerated :exec
-- What a snapshot-time check decided about conversation titles, as
-- MarkMemoriesModerated does about memories: recorded only where the title
-- still hashes to what was checked (a rename meanwhile is another text).
UPDATE conversations c
SET title_checked_hash = CASE WHEN d.verdict = 'ok' THEN h.hash ELSE c.title_checked_hash END,
    title_blocked_hash = CASE WHEN d.verdict = 'blocked' THEN h.hash ELSE c.title_blocked_hash END
FROM unnest(sqlc.arg('ids')::uuid[]) WITH ORDINALITY AS i (id, n)
         JOIN unnest(sqlc.arg('hashes')::text[]) WITH ORDINALITY AS h (hash, n) USING (n)
         JOIN unnest(sqlc.arg('verdicts')::text[]) WITH ORDINALITY AS d (verdict, n) USING (n)
WHERE c.user_id = sqlc.arg('user_id')
  AND c.id = i.id
  AND encode(sha256(convert_to(c.title, 'UTF8')), 'hex') = h.hash;

-- name: GetConversationTitleCheck :one
-- A conversation's title and the hashes recorded about it, for telling whether
-- a title may be shown to the model.
SELECT title, title_checked_hash, title_blocked_hash
FROM conversations
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id');

-- name: RemoveFromRecentChats :exec
-- Takes conversations out of the lists of recent ones in all of a person's
-- conversations' snapshots (docs/design/chat-history-plan.md 3.4), in the
-- transaction that deletes them. The other entries and their refs stay as
-- they are; only snapshots that list one of them are written.
UPDATE conversations c
SET memory_snapshot = jsonb_set(c.memory_snapshot, '{recent}', COALESCE((
    SELECT jsonb_agg(e.entry ORDER BY e.n)
    FROM jsonb_array_elements(c.memory_snapshot -> 'recent') WITH ORDINALITY AS e (entry, n)
    WHERE NOT ((e.entry ->> 'id')::uuid = ANY (sqlc.arg('ids')::uuid[]))
), '[]'::jsonb))
WHERE c.user_id = sqlc.arg('user_id')
  AND jsonb_typeof(c.memory_snapshot -> 'recent') = 'array'
  AND EXISTS (SELECT 1
              FROM jsonb_array_elements(c.memory_snapshot -> 'recent') AS e (entry)
              WHERE (e.entry ->> 'id')::uuid = ANY (sqlc.arg('ids')::uuid[]));
