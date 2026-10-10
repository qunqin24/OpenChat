-- Deletions that take files with them. An attachment row is the only thing
-- that says a stored file is wanted, so each statement hands back the storage
-- keys of the rows it removed, in the same statement: the caller deletes the
-- files once the rows are gone, and no other server can be handed the same
-- keys, because only the one whose DELETE removed a row gets it back.

-- name: DeleteExpiredTemporaryConversations :many
-- Soft-deleted ones go too — a day is a day. Messages, runs and attachments
-- follow by cascade; usage rows are kept (their conversation reference is SET
-- NULL). SKIP LOCKED lets servers run this at once without waiting on each
-- other, and a batch keeps one pass from holding a million rows.
WITH gone AS (
    DELETE FROM conversations
        WHERE id IN (SELECT c.id
                     FROM conversations c
                     WHERE c.temporary
                       AND c.created_at < sqlc.arg('before')
                     ORDER BY c.created_at
                     LIMIT sqlc.arg('row_limit')
                         FOR UPDATE SKIP LOCKED)
        RETURNING id),
     files AS (SELECT m.conversation_id, a.storage_key
               FROM attachments a
                        JOIN messages m ON m.id = a.message_id
               WHERE m.conversation_id IN (SELECT id FROM gone))
SELECT gone.id AS conversation_id,
       COALESCE(array_agg(files.storage_key) FILTER (WHERE files.storage_key IS NOT NULL), '{}')::text[] AS storage_keys
FROM gone
         LEFT JOIN files ON files.conversation_id = gone.id
GROUP BY gone.id;

-- name: PurgeDeletedConversations :many
-- The hard delete behind a user's delete: a conversation they deleted stays
-- recoverable by an operator for the retention period, then goes with its
-- messages, runs and attachments. Same shape as the temporary-chat statement.
WITH gone AS (
    DELETE FROM conversations
        WHERE id IN (SELECT c.id
                     FROM conversations c
                     WHERE c.deleted_at IS NOT NULL
                       AND c.deleted_at < sqlc.arg('before')
                     ORDER BY c.deleted_at
                     LIMIT sqlc.arg('row_limit')
                         FOR UPDATE SKIP LOCKED)
        RETURNING id),
     files AS (SELECT m.conversation_id, a.storage_key
               FROM attachments a
                        JOIN messages m ON m.id = a.message_id
               WHERE m.conversation_id IN (SELECT id FROM gone))
SELECT gone.id AS conversation_id,
       COALESCE(array_agg(files.storage_key) FILTER (WHERE files.storage_key IS NOT NULL), '{}')::text[] AS storage_keys
FROM gone
         LEFT JOIN files ON files.conversation_id = gone.id
GROUP BY gone.id;

-- name: DeleteStaleUploads :many
-- Uploads no message ever claimed: the user attached a file and never sent, or
-- the send failed. Anything this old has no request in flight that could still
-- claim it.
DELETE FROM attachments
WHERE id IN (SELECT a.id
             FROM attachments a
             WHERE a.message_id IS NULL
               AND a.created_at < sqlc.arg('before')
             ORDER BY a.created_at
             LIMIT sqlc.arg('row_limit')
                 FOR UPDATE SKIP LOCKED)
RETURNING storage_key;

-- name: ListKnownStorageKeys :many
-- Which of these keys an attachment row still wants: what the orphan sweep
-- asks before it removes a file from the disk.
SELECT storage_key
FROM attachments
WHERE storage_key = ANY (sqlc.arg('keys')::text[]);
