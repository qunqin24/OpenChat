-- name: GetContextSummary :one
-- The conversation's summary, with where the last row it covers sits in the
-- transcript: rows after that place are the ones a request still carries.
SELECT s.*,
       t.created_at AS through_created_at
FROM context_summaries s
         JOIN messages t ON t.id = s.through_message_id
WHERE s.conversation_id = $1;

-- name: UpsertContextSummary :one
-- Writes the conversation's summary, replacing the one it had. The foreign keys
-- refuse a row that an edit deleted while the summary was being written.
INSERT INTO context_summaries (conversation_id, through_message_id, kept_message_id, content, model_id,
                               input_tokens, output_tokens)
VALUES (sqlc.arg('conversation_id'), sqlc.arg('through_message_id'), sqlc.narg('kept_message_id'),
        sqlc.arg('content'), sqlc.arg('model_id'), sqlc.arg('input_tokens'), sqlc.arg('output_tokens'))
ON CONFLICT (conversation_id) DO UPDATE
    SET through_message_id = EXCLUDED.through_message_id,
        kept_message_id    = EXCLUDED.kept_message_id,
        content            = EXCLUDED.content,
        model_id           = EXCLUDED.model_id,
        input_tokens       = EXCLUDED.input_tokens,
        output_tokens      = EXCLUDED.output_tokens,
        created_at         = now()
RETURNING *;

-- name: DeleteContextSummary :execrows
DELETE
FROM context_summaries
WHERE conversation_id = $1;

-- name: ListMessagesAfter :many
-- Rows after a place in the transcript, oldest first: what a summary has not
-- covered. With no place given, from the start. The rows nearest the end are
-- kept when there are more than the limit - the same choice a request makes,
-- which is answered from the end of the conversation.
SELECT *
FROM (SELECT *
      FROM messages
      WHERE messages.conversation_id = sqlc.arg('conversation_id')
        AND (sqlc.narg('after_created_at')::timestamptz IS NULL
          OR (created_at, id) > (sqlc.narg('after_created_at')::timestamptz, sqlc.narg('after_id')::uuid))
      ORDER BY created_at DESC, id DESC
      LIMIT sqlc.arg('row_limit')) recent
ORDER BY created_at, id;

-- name: ListMessagesFrom :many
-- Rows after a place in the transcript, oldest first, from the oldest: for
-- compressing, which reads what has not been covered from where it begins and
-- stops at the limit, leaving the rest for the next time.
SELECT *
FROM messages
WHERE messages.conversation_id = sqlc.arg('conversation_id')
  AND (sqlc.narg('after_created_at')::timestamptz IS NULL
    OR (created_at, id) > (sqlc.narg('after_created_at')::timestamptz, sqlc.narg('after_id')::uuid))
ORDER BY created_at, id
LIMIT sqlc.arg('row_limit');
