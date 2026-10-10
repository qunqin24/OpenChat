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
-- turns the page around. The transcript is the branch shown (see
-- conversation_transcript), so the cursor walks back along it.
SELECT *
FROM messages
WHERE messages.conversation_id = sqlc.arg('conversation_id')
  AND messages.id IN (SELECT conversation_transcript(sqlc.arg('conversation_id')))
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
-- A user message is written with the input it follows (`parent_input_id`, null
-- for a conversation's first) and then made the conversation's current leaf
-- with SetCurrentInput; no other row has a parent.
--
-- `id` and `created_at` are for guest mode, which writes a transcript the
-- browser kept, under the ids and in the order the browser has it; every other
-- caller leaves both out.
INSERT INTO messages (id, conversation_id, role, content, parts, status, run_id,
                      stop_reason, model, provider, usage, error_code, edit_of, parent_input_id,
                      created_at, updated_at)
VALUES (COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()),
        sqlc.arg('conversation_id'), sqlc.arg('role'), sqlc.arg('content'), sqlc.arg('parts'),
        sqlc.arg('status'), sqlc.narg('run_id'), sqlc.narg('stop_reason'), sqlc.narg('model'),
        sqlc.narg('provider'), sqlc.narg('usage'), sqlc.narg('error_code'), sqlc.narg('edit_of'),
        sqlc.narg('parent_input_id'),
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
-- The newest rows of the branch shown, oldest first: the raw material of a
-- model request. Newest, not oldest — a long conversation is answered from its
-- end. Only the path counts as history: what followed a version set aside by an
-- edit is not, and neither is an answer that was regenerated.
SELECT *
FROM (SELECT *
      FROM messages
      WHERE messages.conversation_id = sqlc.arg('conversation_id')
        AND messages.id IN (SELECT conversation_transcript(sqlc.arg('conversation_id')))
      ORDER BY created_at DESC, id DESC
      LIMIT sqlc.arg('row_limit')) recent
ORDER BY created_at, id;

-- name: ListTranscript :many
-- The branch shown, oldest first and whole, for an export. Tool results are
-- left out: they are the model's working, and some are whole web pages.
SELECT *
FROM messages
WHERE messages.conversation_id = sqlc.arg('conversation_id')
  AND messages.id IN (SELECT conversation_transcript(sqlc.arg('conversation_id')))
  AND messages.role <> 'tool'
ORDER BY created_at, id;

-- name: GetCurrentInput :one
-- The conversation's last user message on the branch shown: the one a
-- regenerated answer answers, and where a new message is appended.
SELECT messages.*
FROM messages
         JOIN conversations c ON c.current_input_id = messages.id
WHERE c.id = $1;

-- name: GetInput :one
-- A user message of the conversation, wherever it is in the tree.
SELECT *
FROM messages
WHERE id = sqlc.arg('id')
  AND conversation_id = sqlc.arg('conversation_id')
  AND role = 'user';

-- name: InputOnPath :one
SELECT EXISTS (SELECT 1 FROM conversation_path(sqlc.arg('conversation_id')) AS p WHERE p = sqlc.arg('id')::uuid) AS on_path;

-- name: PathHasVersionOf :one
-- Whether the branch shown passes through a version of this question: a
-- message with the same parent, itself included. Those are the only messages
-- the reader can switch to.
SELECT EXISTS (SELECT 1
               FROM messages m
               WHERE m.id IN (SELECT conversation_path(sqlc.arg('conversation_id')))
                 AND m.parent_input_id IS NOT DISTINCT FROM sqlc.narg('parent_input_id')::uuid) AS found;

-- name: ListVersions :many
-- For each of these user messages, the versions of its question: every user
-- message with the same parent (itself included), oldest first. Roots have no
-- parent, so a conversation's first questions are found among its own.
SELECT m.id AS message_id, s.id AS version_id
FROM messages m
         CROSS JOIN LATERAL (SELECT x.id, x.created_at
                             FROM messages x
                             WHERE x.parent_input_id = m.parent_input_id
                             UNION ALL
                             SELECT x.id, x.created_at
                             FROM messages x
                             WHERE m.parent_input_id IS NULL
                               AND x.conversation_id = m.conversation_id
                               AND x.role = 'user'
                               AND x.parent_input_id IS NULL) s
WHERE m.id = ANY (sqlc.arg('ids')::uuid[])
ORDER BY m.id, s.created_at, s.id;

-- name: PickBranchLeaf :one
-- Where the reader lands when switching to this question: the end of the
-- branch below it that was last the one shown.
WITH RECURSIVE below (id) AS (SELECT sqlc.arg('id')::uuid
                              UNION
                              SELECT m.id
                              FROM messages m
                                       JOIN below ON m.parent_input_id = below.id)
SELECT b.id::uuid AS id
FROM below b
         JOIN messages q ON q.id = b.id
WHERE NOT EXISTS (SELECT 1 FROM messages c WHERE c.parent_input_id = b.id)
ORDER BY q.shown_at DESC NULLS LAST, q.created_at DESC, b.id DESC
LIMIT 1;

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
