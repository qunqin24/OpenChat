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
