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

-- name: AppendEntry :one
-- The only way a transcript row is written by the agent layer. Rows are never
-- updated afterwards: a reply still being generated lives in runs.live, and
-- becomes a row only when it is finished.
--
-- created_at is the wall clock, not now(): now() is the transaction's start, so
-- two rows written by one commit would tie and fall back to ordering by a
-- random uuid.
INSERT INTO messages (conversation_id, role, content, parts, status, run_id,
                      stop_reason, model, provider, usage, error_code, created_at, updated_at)
VALUES (sqlc.arg('conversation_id'), sqlc.arg('role'), sqlc.arg('content'), sqlc.arg('parts'),
        sqlc.arg('status'), sqlc.narg('run_id'), sqlc.narg('stop_reason'), sqlc.narg('model'),
        sqlc.narg('provider'), sqlc.narg('usage'), sqlc.narg('error_code'),
        clock_timestamp(), clock_timestamp())
RETURNING *;

-- name: ListRunMessages :many
-- Every row a run wrote, its input included: what a subscriber's snapshot
-- carries alongside the run's live state.
SELECT *
FROM messages
WHERE run_id = sqlc.arg('run_id')
   OR id = sqlc.arg('input_message_id')
ORDER BY created_at, id;

-- name: ListRecentMessages :many
-- The newest rows of a conversation, oldest first: the raw material of a model
-- request. Newest, not oldest — a long conversation is answered from its end.
SELECT *
FROM (SELECT *
      FROM messages
      WHERE conversation_id = sqlc.arg('conversation_id')
      ORDER BY created_at DESC, id DESC
      LIMIT sqlc.arg('row_limit')) recent
ORDER BY created_at, id;

-- name: GetMessage :one
SELECT *
FROM messages
WHERE id = $1;
