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
