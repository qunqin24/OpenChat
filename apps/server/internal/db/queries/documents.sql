-- Writing blocks (api/documents.go). Every statement names the owner, so
-- another person's document reads as missing.

-- name: CreateDocument :one
INSERT INTO documents (conversation_id, user_id, run_id)
VALUES (sqlc.arg('conversation_id'), sqlc.arg('user_id'), sqlc.arg('run_id'))
RETURNING *;

-- name: InsertDocumentVersion :one
-- The next version of a document. The caller holds the document's row lock
-- (LockDocument), so two changes at once are numbered one after the other.
INSERT INTO document_versions (document_id, version, title, content, source, run_id, checked)
VALUES (sqlc.arg('document_id'),
        COALESCE((SELECT max(v.version) FROM document_versions v WHERE v.document_id = sqlc.arg('document_id')), 0) + 1,
        sqlc.arg('title'), sqlc.arg('content'), sqlc.arg('source'), sqlc.narg('run_id'), sqlc.narg('checked'))
RETURNING *;

-- name: LockDocument :one
-- A document of the person's, locked for a change, with its conversation.
SELECT d.*
FROM documents d
         JOIN conversations c ON c.id = d.conversation_id
WHERE d.id = sqlc.arg('id')
  AND d.user_id = sqlc.arg('user_id')
  AND c.deleted_at IS NULL
    FOR UPDATE OF d;

-- name: GetDocument :one
SELECT d.*
FROM documents d
         JOIN conversations c ON c.id = d.conversation_id
WHERE d.id = sqlc.arg('id')
  AND d.user_id = sqlc.arg('user_id')
  AND c.deleted_at IS NULL;

-- name: LatestDocumentVersion :one
SELECT *
FROM document_versions
WHERE document_id = sqlc.arg('document_id')
ORDER BY version DESC
LIMIT 1;

-- name: ListDocumentVersions :many
-- Oldest first: the panel steps through them.
SELECT *
FROM document_versions
WHERE document_id = sqlc.arg('document_id')
ORDER BY version;

-- name: ListConversationDocuments :many
-- A conversation's documents, each at its latest version.
SELECT d.id, d.created_at, v.version, v.title, v.source, length(v.content)::int AS length, v.created_at AS updated_at
FROM documents d
         JOIN LATERAL (SELECT * FROM document_versions dv WHERE dv.document_id = d.id ORDER BY dv.version DESC LIMIT 1) v ON true
WHERE d.conversation_id = sqlc.arg('conversation_id')
  AND d.user_id = sqlc.arg('user_id')
ORDER BY d.created_at;

-- name: SetDocumentVersionChecked :exec
UPDATE document_versions
SET checked = sqlc.arg('checked')
WHERE document_id = sqlc.arg('document_id')
  AND version = sqlc.arg('version');

-- name: GetDocumentVersion :one
SELECT *
FROM document_versions
WHERE document_id = sqlc.arg('document_id')
  AND version = sqlc.arg('version');

-- name: LatestModelDocumentVersion :one
-- The last version a model wrote: what it last knew of the text, unless it
-- has read the document since.
SELECT *
FROM document_versions
WHERE document_id = sqlc.arg('document_id')
  AND source = 'model'
ORDER BY version DESC
LIMIT 1;
