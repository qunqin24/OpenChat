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
-- A page of the transcript from its end: the newest rows, or with a cursor the
-- rows older than it. Newest first, so the limit keeps the end; the caller
-- turns the page around. The transcript is every row of the conversation: what
-- is replaced is deleted, so there is nothing else in it.
SELECT *
FROM messages
WHERE messages.conversation_id = sqlc.arg('conversation_id')
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: AppendEntry :one
-- The only way a transcript row is written by the agent layer. Rows are never
-- updated afterwards: a reply still being generated lives in runs.live, and
-- becomes a row only when it is finished.
--
-- created_at is the wall clock, not now(): now() is the transaction's start, so
-- two rows written by one commit would tie and fall back to ordering by a
-- random uuid.
--
-- `id` and `created_at` are for guest mode, which writes a transcript the
-- browser kept, under the ids and in the order the browser has it; every other
-- caller leaves both out.
INSERT INTO messages (id, conversation_id, role, content, parts, status, run_id,
                      stop_reason, model, provider, usage, error_code,
                      created_at, updated_at)
VALUES (COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()),
        sqlc.arg('conversation_id'), sqlc.arg('role'), sqlc.arg('content'), sqlc.arg('parts'),
        sqlc.arg('status'), sqlc.narg('run_id'), sqlc.narg('stop_reason'), sqlc.narg('model'),
        sqlc.narg('provider'), sqlc.narg('usage'), sqlc.narg('error_code'),
        COALESCE(sqlc.narg('created_at')::timestamptz, clock_timestamp()),
        COALESCE(sqlc.narg('created_at')::timestamptz, clock_timestamp()))
RETURNING *;

-- name: ListRunMessages :many
-- Every row a run wrote, its input included: what a subscriber's snapshot
-- carries alongside the run's live state. The runs that continue it (the answer
-- after a question) are its rows too.
SELECT *
FROM messages
WHERE messages.run_id = sqlc.arg('run_id')
   OR messages.run_id IN (SELECT r.id FROM runs r WHERE r.continues = sqlc.arg('run_id'))
   OR messages.id = sqlc.arg('input_message_id')
ORDER BY created_at, id;

-- name: ListRecentMessages :many
-- The newest rows of the conversation, oldest first: the raw material of a
-- model request. Newest, not oldest — a long conversation is answered from its
-- end.
SELECT *
FROM (SELECT *
      FROM messages
      WHERE messages.conversation_id = sqlc.arg('conversation_id')
      ORDER BY created_at DESC, id DESC
      LIMIT sqlc.arg('row_limit')) recent
ORDER BY created_at, id;

-- name: ListTranscript :many
-- The conversation, oldest first and whole, for an export. Tool results are
-- left out: they are the model's working, and some are whole web pages.
SELECT *
FROM messages
WHERE messages.conversation_id = sqlc.arg('conversation_id')
  AND messages.role <> 'tool'
ORDER BY created_at, id;

-- name: GetLastInput :one
-- The conversation's last user message: the one a regenerated answer answers.
SELECT *
FROM messages
WHERE conversation_id = $1
  AND role = 'user'
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: GetInput :one
-- A user message of the conversation.
SELECT *
FROM messages
WHERE id = sqlc.arg('id')
  AND conversation_id = sqlc.arg('conversation_id')
  AND role = 'user';

-- name: DeleteTranscriptFrom :many
-- Deletes the row given and every row after it in the conversation, except
-- one to keep (the question that replaces the first of them, written before
-- this runs). That is what editing a question does: it and everything after
-- it is gone for good. The runs of the questions deleted go with them
-- (runs.input_message_id cascades), while a run's other rows only lose their
-- run_id, which is why every row is deleted by its place and not by its run.
-- Hands back the storage keys of the files that were attached, in the same
-- statement, for the caller to delete once this commits (cleanup.go).
WITH gone AS (
    DELETE FROM messages
        WHERE messages.conversation_id = sqlc.arg('conversation_id')
            AND (messages.created_at, messages.id) >= (sqlc.arg('from_created_at')::timestamptz, sqlc.arg('from_id')::uuid)
            AND messages.id <> sqlc.arg('keep_id')::uuid
        RETURNING messages.id)
SELECT a.storage_key
FROM attachments a
WHERE a.message_id IN (SELECT id FROM gone);

-- name: DeleteAnswerTo :exec
-- Deletes the answer to a question: every run on it - the ones that carry on
-- after a question the model asked included - and every row that follows it.
-- A question is only regenerated while it is the last one, so what follows it
-- is its answer. Files hang on user messages only, so there are none to hand
-- back.
WITH rows AS (
    DELETE FROM messages
        USING messages question
        WHERE question.id = sqlc.arg('input_id')
            AND messages.conversation_id = sqlc.arg('conversation_id')
            AND messages.role <> 'user'
            AND (messages.created_at, messages.id) > (question.created_at, question.id)
        RETURNING messages.id)
DELETE
FROM runs
WHERE runs.input_message_id = sqlc.arg('input_id');

-- name: MoveAttachments :exec
-- Hands the files of one message to another: an edited question's files go to
-- the question that replaces it.
UPDATE attachments
SET message_id = sqlc.arg('to_id')
WHERE message_id = sqlc.arg('from_id');

-- name: GetMessage :one
SELECT *
FROM messages
WHERE id = $1;

-- name: ToolResultMentions :one
-- Whether any tool result in the conversation contains the text, for the
-- image proxy: it serves only addresses a page or search put in front of the
-- model. jsonb renders strings with only quotes and backslashes escaped, so
-- an http(s) address matches as written.
SELECT EXISTS (
    SELECT 1
    FROM messages
    WHERE conversation_id = sqlc.arg('conversation_id')
      AND role = 'tool'
      AND position(sqlc.arg('needle')::text IN parts::text) > 0
) AS found;
