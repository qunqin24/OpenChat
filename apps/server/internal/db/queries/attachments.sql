-- name: CreateAttachment :one
INSERT INTO attachments (user_id, storage_key, filename, content_type, size_bytes)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListPendingAttachments :many
-- The caller's own uploads that no message has claimed yet.
SELECT *
FROM attachments
WHERE id = ANY (sqlc.arg('ids')::uuid[])
  AND user_id = sqlc.arg('user_id')
  AND message_id IS NULL
ORDER BY created_at, id;

-- name: AttachToMessage :execrows
-- Claims uploads for a message. Only the caller's own, only unclaimed ones: the
-- row count is how the caller notices that an id did not qualify.
UPDATE attachments
SET message_id = sqlc.arg('message_id')
WHERE id = ANY (sqlc.arg('ids')::uuid[])
  AND user_id = sqlc.arg('user_id')
  AND message_id IS NULL;

-- name: ListAttachmentsByMessages :many
SELECT *
FROM attachments
WHERE message_id = ANY (sqlc.arg('message_ids')::uuid[])
ORDER BY created_at, id;

-- name: ListAttachmentsForUser :many
-- Attachments by id, within one user's scope: what the context builder resolves
-- a reference through, so a reference to someone else's file reads as missing.
SELECT *
FROM attachments
WHERE id = ANY (sqlc.arg('ids')::uuid[])
  AND user_id = sqlc.arg('user_id');

-- name: GetAttachmentByStorageKey :one
-- What the local storage driver's handlers know a file by. The signature in
-- the URL is what authorises the request, so there is no owner to check.
SELECT *
FROM attachments
WHERE storage_key = $1;

-- name: ListLibraryFiles :many
-- The library: the files a user sent, newest first, each with the conversation
-- it was sent in. Only conversations the user can still open - not deleted
-- ones, not temporary ones, which are not history - and only the user's own,
-- on both the file and the conversation.
SELECT a.*,
       c.id    AS conversation_id,
       c.title AS conversation_title
FROM attachments a
         JOIN messages m ON m.id = a.message_id
         JOIN conversations c ON c.id = m.conversation_id
WHERE a.user_id = sqlc.arg('user_id')
  AND c.user_id = sqlc.arg('user_id')
  AND c.deleted_at IS NULL
  AND NOT c.temporary
  AND (sqlc.narg('content_type_like')::text IS NULL OR a.content_type LIKE sqlc.narg('content_type_like')::text)
  AND (sqlc.narg('pattern')::text IS NULL OR a.filename ILIKE sqlc.narg('pattern')::text)
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (a.created_at, a.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg('row_limit');

-- name: GetLibraryFile :one
-- One file of the library, in the same scope as ListLibraryFiles.
SELECT a.*
FROM attachments a
         JOIN messages m ON m.id = a.message_id
         JOIN conversations c ON c.id = m.conversation_id
WHERE a.id = sqlc.arg('id')
  AND a.user_id = sqlc.arg('user_id')
  AND c.user_id = sqlc.arg('user_id')
  AND c.deleted_at IS NULL
  AND NOT c.temporary;

-- name: DeleteLibraryFile :one
-- Removes a file of the library, in its scope. The message keeps naming the
-- file in its parts, which is how it is shown as deleted and how the model is
-- told it is gone (agent.attachmentResolver). The storage key comes back for
-- the caller to delete the file once the row is gone, and the conversation for
-- it to tell the pages showing it.
WITH target AS (SELECT a.id, m.conversation_id
                FROM attachments a
                         JOIN messages m ON m.id = a.message_id
                         JOIN conversations c ON c.id = m.conversation_id
                WHERE a.id = sqlc.arg('id')
                  AND a.user_id = sqlc.arg('user_id')
                  AND c.user_id = sqlc.arg('user_id')
                  AND c.deleted_at IS NULL
                  AND NOT c.temporary),
     gone AS (DELETE FROM attachments
         WHERE id = (SELECT id FROM target)
         RETURNING storage_key)
SELECT gone.storage_key, target.conversation_id
FROM gone, target;

-- name: ListConversationTextFileMessages :many
-- The person's messages in a conversation that carry files read as words -
-- text files, Word, Excel and PDF - oldest first: what the file tools
-- (agent/files.go) look files up in. A refused message's files are never read
-- for the model.
SELECT m.*
FROM messages m
WHERE m.conversation_id = $1
  AND m.role = 'user'
  AND m.status <> 'blocked'
  AND EXISTS (
    SELECT 1 FROM attachments a
    WHERE a.message_id = m.id
      AND a.content_type IN ('text/plain', 'application/pdf',
        'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet')
  )
ORDER BY m.created_at, m.id;

-- name: LockUserStorage :exec
-- Held while a person's files are counted against their quota and one is
-- added, so two uploads at once cannot both fit in the room left for one.
SELECT pg_advisory_xact_lock(hashtextextended('user-storage:' || sqlc.arg('user_id')::uuid::text, 0));

-- name: UserStorageByKind :many
-- What a person's files take, by kind: everything they uploaded, sent or not
-- yet, in kept or temporary conversations alike - all of it is stored.
SELECT (CASE
            WHEN content_type LIKE 'image/%' THEN 'image'
            WHEN content_type = 'application/pdf' THEN 'pdf'
            WHEN content_type LIKE 'application/vnd.openxmlformats-officedocument.%' THEN 'document'
            WHEN content_type = 'text/plain' THEN 'text'
            ELSE 'other'
        END)::text AS kind,
       count(*) AS files,
       COALESCE(sum(size_bytes), 0)::bigint AS bytes
FROM attachments
WHERE user_id = sqlc.arg('user_id')
GROUP BY 1;

-- name: UserStorageUsed :one
SELECT COALESCE(sum(size_bytes), 0)::bigint
FROM attachments
WHERE user_id = sqlc.arg('user_id');

-- name: UsersStorageUsed :many
-- The admin users table's column: what each of a page of accounts keeps.
SELECT user_id, COALESCE(sum(size_bytes), 0)::bigint AS bytes
FROM attachments
WHERE user_id = ANY (sqlc.arg('user_ids')::uuid[])
GROUP BY user_id;

-- name: SetAttachmentSize :exec
-- An upload's size as stored, over the one its uploader announced, before a
-- message claims it.
UPDATE attachments
SET size_bytes = sqlc.arg('size_bytes')
WHERE id = sqlc.arg('id')
  AND message_id IS NULL;
