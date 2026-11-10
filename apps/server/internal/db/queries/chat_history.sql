-- The chat_history tool's reads (docs/design/chat-history-plan.md, section 4).
-- Every query is the person's own, searchable conversations only: kind 'chat',
-- not temporary, not deleted, never the one the tool is called from, and only
-- what a person or a model said (user and assistant messages) that moderation
-- did not refuse and that nothing but them wrote (origin IS NULL).

-- name: SearchHistoryMessages :many
-- Messages containing any of the patterns (ILIKE patterns, escaped by the
-- caller), newest first, with their size for the ranking. Narrowed to a time
-- range when given (to_at is exclusive) and to conversations not in skip_ids.
--
-- Each keyword keeps its own newest per_keyword matches: a common word of the
-- search cannot crowd out the two messages that hold the rare one. One pass
-- over the person's messages finds the matches; telling which keyword a match
-- holds reads only the matches again. The content is cut at 20000 characters
-- (the ranking and the excerpt read no more); the length is in bytes, which
-- costs nothing to know.
WITH matched AS MATERIALIZED (
    SELECT m.id, m.conversation_id, m.role, m.content, m.created_at
    FROM messages m
             JOIN conversations c ON c.id = m.conversation_id
    WHERE c.user_id = sqlc.arg('user_id')
      AND c.deleted_at IS NULL
      AND c.kind = 'chat'
      AND NOT c.temporary
      AND c.id <> sqlc.arg('current_id')
      AND NOT (c.id = ANY (sqlc.arg('skip_ids')::uuid[]))
      AND m.role IN ('user', 'assistant')
      AND m.status <> 'blocked'
      AND m.origin IS NULL
      AND m.content ILIKE ANY (sqlc.arg('patterns')::text[])
      AND (sqlc.narg('from_at')::timestamptz IS NULL OR m.created_at >= sqlc.narg('from_at')::timestamptz)
      AND (sqlc.narg('to_at')::timestamptz IS NULL OR m.created_at < sqlc.narg('to_at')::timestamptz)
),
     placed AS (
         SELECT x.id,
                row_number() OVER (PARTITION BY p.n ORDER BY x.created_at DESC, x.id DESC) AS place
         FROM matched x
                  JOIN unnest(sqlc.arg('patterns')::text[]) WITH ORDINALITY AS p (pattern, n)
                       ON x.content ILIKE p.pattern
     )
SELECT x.id, x.conversation_id, x.role, left(x.content, 20000)::text AS content,
       octet_length(x.content)::int AS length, x.created_at
FROM matched x
WHERE x.id IN (SELECT placed.id FROM placed WHERE placed.place <= sqlc.arg('per_keyword')::int)
ORDER BY x.created_at DESC, x.id DESC;

-- name: HistoryCorpusStats :one
-- What the ranking's length normalisation and IDF need of the whole history:
-- how many searchable messages the person has and their average size in bytes.
-- No message's text is read for it.
SELECT count(*)::bigint AS total,
       COALESCE(avg(octet_length(m.content)), 0)::float8 AS avg_length
FROM messages m
         JOIN conversations c ON c.id = m.conversation_id
WHERE c.user_id = sqlc.arg('user_id')
  AND c.deleted_at IS NULL
  AND c.kind = 'chat'
  AND NOT c.temporary
  AND c.id <> sqlc.arg('current_id')
  AND m.role IN ('user', 'assistant')
  AND m.status <> 'blocked'
  AND m.origin IS NULL;

-- name: ListHistoryContext :many
-- The messages within two places of a hit in its conversation (counted among
-- the searchable messages), each with its place n. A message near two hits
-- comes once. The places are worked out without the text; only the messages
-- shown are read.
WITH numbered AS (
    SELECT m.id, m.conversation_id,
           row_number() OVER (PARTITION BY m.conversation_id ORDER BY m.created_at, m.id)::int AS n
    FROM messages m
             JOIN conversations c ON c.id = m.conversation_id
    WHERE c.user_id = sqlc.arg('user_id')
      AND c.deleted_at IS NULL
      AND c.kind = 'chat'
      AND NOT c.temporary
      AND c.id <> sqlc.arg('current_id')
      AND c.id IN (SELECT h.conversation_id FROM messages h WHERE h.id = ANY (sqlc.arg('hit_ids')::uuid[]))
      AND m.role IN ('user', 'assistant')
      AND m.status <> 'blocked'
      AND m.origin IS NULL
),
     shown AS (
         SELECT DISTINCT x.id, x.conversation_id, x.n
         FROM numbered h
                  JOIN numbered x ON x.conversation_id = h.conversation_id AND x.n BETWEEN h.n - 2 AND h.n + 2
         WHERE h.id = ANY (sqlc.arg('hit_ids')::uuid[])
     )
SELECT shown.id, shown.conversation_id, m.role, left(m.content, 4000)::text AS content, m.created_at, shown.n
FROM shown
         JOIN messages m ON m.id = shown.id
ORDER BY shown.conversation_id, shown.n;

-- name: GetHistoryConversation :one
-- A conversation the tool may read and how many searchable messages it has.
-- Someone else's, a temporary or deleted one, a sub-agent's and the current
-- conversation are simply not found.
SELECT c.id, count(m.id)::bigint AS message_count
FROM conversations c
         LEFT JOIN messages m ON m.conversation_id = c.id
    AND m.role IN ('user', 'assistant')
    AND m.status <> 'blocked'
    AND m.origin IS NULL
WHERE c.id = sqlc.arg('id')
  AND c.user_id = sqlc.arg('user_id')
  AND c.deleted_at IS NULL
  AND c.kind = 'chat'
  AND NOT c.temporary
  AND c.id <> sqlc.arg('current_id')
GROUP BY c.id;

-- name: ListHistoryMessages :many
-- A page of a conversation's searchable messages in order. The caller has
-- checked the conversation with GetHistoryConversation.
SELECT m.role, left(m.content, 20000)::text AS content, m.created_at
FROM messages m
WHERE m.conversation_id = sqlc.arg('conversation_id')
  AND m.role IN ('user', 'assistant')
  AND m.status <> 'blocked'
  AND m.origin IS NULL
ORDER BY m.created_at, m.id
OFFSET sqlc.arg('row_offset')
LIMIT sqlc.arg('row_limit');
