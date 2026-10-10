-- Guest mode. A guest has no account: each run is executed against rows that
-- are written when the request arrives and deleted when it ends (see
-- api/guest_run.go). Everything here is either that write, that delete, or the
-- counting behind the limits an administrator sets.

-- name: CreateGuestUser :one
-- The throwaway owner of one run's rows. No password and no session: it can
-- never sign in, and the queries behind sign-in, the user list and the user
-- counts leave `guest` rows out. The personalization is the guest's own, kept
-- in their browser and sent with the request.
INSERT INTO users (email, name, password_hash, role, guest, instructions_about, instructions_style, memory_enabled)
VALUES (sqlc.arg('email'), sqlc.arg('name'), '', 'user', true,
        sqlc.arg('instructions_about'), sqlc.arg('instructions_style'), sqlc.arg('memory_enabled'))
RETURNING *;

-- name: IsGuestUser :one
SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND guest) AS guest;

-- name: CreateGuestConversation :one
-- The conversation under the id the browser knows it by, with the memory
-- snapshot the browser kept from its first run (NULL: this is the first).
INSERT INTO conversations (id, user_id, title, model, reasoning_effort, web_search, temporary, memory_snapshot)
VALUES (sqlc.arg('id'), sqlc.arg('user_id'), sqlc.arg('title'), sqlc.narg('model'), sqlc.narg('reasoning_effort'),
        sqlc.arg('web_search'), sqlc.arg('temporary'), sqlc.narg('memory_snapshot')::jsonb)
RETURNING *;

-- name: CreateGuestAttachment :one
-- A file the browser sent with a question, under the id its message refers to.
INSERT INTO attachments (id, message_id, user_id, storage_key, filename, content_type, size_bytes)
VALUES (sqlc.arg('id'), sqlc.arg('message_id'), sqlc.arg('user_id'), sqlc.arg('storage_key'), sqlc.arg('filename'),
        sqlc.arg('content_type'), sqlc.arg('size_bytes'))
RETURNING *;

-- name: CreateGuestStubRun :one
-- The run that asked a question, standing in for the one the browser answers:
-- the run that goes on after an answer `continues` it, and says so by its id.
-- The stub holds no rows (the question is a transcript row of the shape older
-- conversations have) and is never executed.
INSERT INTO runs (id, conversation_id, user_id, input_message_id, model_id, channel_id, status, finished_at)
VALUES (sqlc.arg('id'), sqlc.arg('conversation_id'), sqlc.arg('user_id'), sqlc.arg('input_message_id'),
        sqlc.arg('model_id'), sqlc.narg('channel_id'), 'awaiting_input', now())
RETURNING *;

-- name: RequestGuestRunCancel :one
-- Stop pressed in the guest's page. A guest's run is named by its id alone,
-- which only the page that started it was told, and only a guest's run can be
-- reached this way.
UPDATE runs
SET cancel_requested = true,
    updated_at       = now()
WHERE runs.id = sqlc.arg('id')
  AND runs.status = 'running'
  AND runs.user_id IN (SELECT users.id FROM users WHERE users.guest)
RETURNING *;

-- name: DeleteGuestUser :one
-- The end of a run: the user goes, and the conversation, transcript, runs,
-- attachments and memories go with it by cascade. The usage ledger keeps its
-- rows with the user (and conversation, and run) set to NULL - an account is
-- never deleted otherwise, so a request without an owner is a guest's. The
-- storage keys of the attachments are handed back in the same statement, for
-- the files to be removed after (api/cleanup.go does the same for chats).
WITH files AS (SELECT a.storage_key
               FROM attachments a
               WHERE a.user_id = sqlc.arg('id')
                  OR a.message_id IN (SELECT m.id
                                      FROM messages m
                                               JOIN conversations c ON c.id = m.conversation_id
                                      WHERE c.user_id = sqlc.arg('id'))),
     gone AS (DELETE FROM users u WHERE u.id = sqlc.arg('id') AND u.guest RETURNING u.id)
SELECT COALESCE((SELECT array_agg(DISTINCT files.storage_key) FROM files), '{}')::text[] AS storage_keys
FROM gone;

-- name: DeleteStaleGuestUsers :many
-- Guests whose run should long be over: the process that made them died before
-- it could clean up. Same shape as the other cleanup statements - one DELETE
-- with the age in its WHERE clause, the storage keys handed back, SKIP LOCKED
-- so servers can sweep at once.
WITH stale AS (SELECT u.id
               FROM users u
               WHERE u.guest
                 AND u.created_at < sqlc.arg('before')
               ORDER BY u.created_at
               LIMIT sqlc.arg('row_limit')
                   FOR UPDATE SKIP LOCKED),
     files AS (SELECT c.user_id, a.storage_key
               FROM attachments a
                        JOIN messages m ON m.id = a.message_id
                        JOIN conversations c ON c.id = m.conversation_id
               WHERE c.user_id IN (SELECT id FROM stale)
               UNION
               SELECT a.user_id, a.storage_key
               FROM attachments a
               WHERE a.user_id IN (SELECT id FROM stale)),
     gone AS (DELETE FROM users u WHERE u.id IN (SELECT stale.id FROM stale) RETURNING u.id)
SELECT gone.id AS user_id,
       COALESCE(array_agg(files.storage_key) FILTER (WHERE files.storage_key IS NOT NULL), '{}')::text[] AS storage_keys
FROM gone
         LEFT JOIN files ON files.user_id = gone.id
GROUP BY gone.id;

-- name: LockGuestRequests :exec
-- Held until the transaction ends, so that counting and then recording a
-- request is one step: two requests at the limit cannot both be let through.
SELECT pg_advisory_xact_lock(hashtextextended('guest_requests', 11));

-- name: CountGuestRequests :one
-- How many requests this address has made in the last hour, and everyone in
-- the last day.
SELECT (SELECT count(*) FROM guest_requests g WHERE g.ip = sqlc.arg('ip')::text AND g.created_at > now() - interval '1 hour')::bigint AS from_ip,
       (SELECT count(*) FROM guest_requests g WHERE g.created_at > now() - interval '1 day')::bigint                                       AS today;

-- name: InsertGuestRequest :exec
INSERT INTO guest_requests (ip)
VALUES ($1);

-- name: PurgeGuestRequests :execrows
-- Nothing counts a request for more than a day.
DELETE FROM guest_requests
WHERE created_at < sqlc.arg('before');
