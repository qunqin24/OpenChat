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
INSERT INTO messages (conversation_id, role, content, parts, status, run_id,
                      stop_reason, model, provider, usage, error_code, origin,
                      created_at, updated_at)
VALUES (sqlc.arg('conversation_id'), sqlc.arg('role'), sqlc.arg('content'), sqlc.arg('parts'),
        sqlc.arg('status'), sqlc.narg('run_id'), sqlc.narg('stop_reason'), sqlc.narg('model'),
        sqlc.narg('provider'), sqlc.narg('usage'), sqlc.narg('error_code'), sqlc.narg('origin'),
        clock_timestamp(), clock_timestamp())
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
-- end. A message that moderation refused (status 'blocked') is not a row of the
-- context: it is shown to the person and never read back, so it neither fills
-- the window nor hides the question it follows.
SELECT *
FROM (SELECT *
      FROM messages
      WHERE messages.conversation_id = sqlc.arg('conversation_id')
        AND messages.status <> 'blocked'
      ORDER BY created_at DESC, id DESC
      LIMIT sqlc.arg('row_limit')) recent
ORDER BY created_at, id;

-- name: ListTranscript :many
-- The conversation, oldest first and whole, for an export. Tool results are
-- left out: they are the model's working, and some are whole web pages. Nor are
-- the messages moderation refused: they were never sent, and are not a record
-- of the conversation.
SELECT *
FROM messages
WHERE messages.conversation_id = sqlc.arg('conversation_id')
  AND messages.role <> 'tool'
  AND messages.status <> 'blocked'
  AND messages.origin IS NULL
ORDER BY created_at, id;

-- name: GetLastInput :one
-- The conversation's last user message: the one a regenerated answer answers.
-- A message moderation refused is not a question that was asked, so the last
-- question is the one before it; nor is a sub-agent's report, which no person
-- wrote.
SELECT *
FROM messages
WHERE conversation_id = $1
  AND role = 'user'
  AND status <> 'blocked'
  AND origin IS NULL
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: GetLastMessage :one
-- The conversation's last row, whatever it is: where the transcript stops. A
-- conversation whose last row is a refused message (status 'blocked') does not
-- go on until that message is reworded, so every request that would write
-- after it asks this first, with the conversation held.
SELECT *
FROM messages
WHERE conversation_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: GetInput :one
-- A user message of the conversation - a refused one too: it can be edited like
-- any other, and the new wording is checked again. A sub-agent's report is not
-- the person's to edit.
SELECT *
FROM messages
WHERE id = sqlc.arg('id')
  AND conversation_id = sqlc.arg('conversation_id')
  AND role = 'user'
  AND origin IS NULL;

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
-- is its answer - and the messages moderation refused since, and the
-- sub-agents' reports delivered since, which are user rows but not questions.
-- Files hang on user messages that were sent, so neither has any to hand back.
-- A report whose row goes is pending again (subagent_reports, ON DELETE SET
-- NULL), and the new answer receives it.
WITH rows AS (
    DELETE FROM messages
        USING messages question
        WHERE question.id = sqlc.arg('input_id')
            AND messages.conversation_id = sqlc.arg('conversation_id')
            AND (messages.role <> 'user' OR messages.status = 'blocked' OR messages.origin IS NOT NULL)
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

