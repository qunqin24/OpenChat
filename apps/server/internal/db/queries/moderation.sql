-- Content moderation (api/moderation.go): the log of what was turned away, the
-- and the strikes counted from it.

-- name: GetSettings :many
-- Several settings in one round trip: the moderation settings are read on every
-- message, and a request should not cost a query per key.
SELECT key, value
FROM app_settings
WHERE key = ANY (sqlc.arg('keys')::text[]);

-- name: LockModerationSettings :exec
-- Held until the transaction ends: saving the moderation settings reads them,
-- applies the change and writes all of them, and two saves at once would
-- otherwise each start from the same old settings and the later one would
-- undo the earlier one's fields. Servers sharing the database share the lock.
SELECT pg_advisory_xact_lock(hashtextextended('moderation_settings', 31));

-- name: LockModerationSubject :exec
-- Held until the transaction ends, so that recording a blocked message,
-- counting the subject's strikes and sanctioning them is one step: parallel
-- requests of one account or address cannot all count one fewer than the
-- others, and servers sharing the database share the lock.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('subject')::text, 14));

-- name: InsertModerationEvent :one
INSERT INTO moderation_events (user_id, conversation_id, source, mode, categories, excerpt)
VALUES (sqlc.narg('user_id'), sqlc.narg('conversation_id'), sqlc.arg('source'),
        sqlc.arg('mode'), sqlc.arg('categories')::text[], sqlc.arg('excerpt'))
RETURNING *;

-- name: CountUserStrikes :one
-- An account's blocked messages in the window, counted from the last time it
-- was sanctioned: an administrator who enables the account again gives it a
-- fresh start, instead of the next message setting the same punishment off.
-- A refusal at snapshot time is not a strike: it is old text being left out of
-- a conversation, not something the person is sending now. Nor is a task's
-- prompt checked as it is about to be sent, or as a model wrote it (task_run).
SELECT count(*)::bigint
FROM moderation_events e
WHERE e.user_id = sqlc.arg('user_id')
  AND e.source NOT IN ('snapshot', 'task_run')
  AND e.created_at > now() - make_interval(hours => sqlc.arg('window_hours')::int)
  AND e.created_at > COALESCE((SELECT max(s.created_at)
                               FROM moderation_events s
                               WHERE s.user_id = sqlc.arg('user_id')
                                 AND s.sanction IS NOT NULL), '-infinity'::timestamptz);

-- name: MarkModerationSanction :exec
UPDATE moderation_events
SET sanction = sqlc.arg('sanction')
WHERE id = sqlc.arg('id');

-- name: GetModerationEvent :one
SELECT *
FROM moderation_events
WHERE id = $1;

-- name: ListModerationEvents :many
-- Newest first, by keyset on (created_at, id): the order of
-- moderation_events_created_idx. The account's name and whether it is disabled
-- are read alongside.
SELECT e.id,
       e.created_at,
       e.user_id,
       u.name                                       AS user_name,
       u.email                                      AS user_email,
       (u.disabled_at IS NOT NULL)::boolean         AS user_disabled,
       e.conversation_id,
       e.source,
       e.mode,
       e.categories,
       e.excerpt,
       e.sanction
FROM moderation_events e
         LEFT JOIN users u ON u.id = e.user_id
WHERE (sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (e.created_at, e.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
  AND (sqlc.narg('user_id')::uuid IS NULL OR e.user_id = sqlc.narg('user_id')::uuid)
  AND (sqlc.narg('category')::text IS NULL OR e.categories @> ARRAY [sqlc.narg('category')::text])
ORDER BY e.created_at DESC, e.id DESC
LIMIT sqlc.arg('row_limit');

-- name: PurgeModerationEvents :execrows
DELETE
FROM moderation_events
WHERE id IN (SELECT m.id FROM moderation_events m WHERE m.created_at < sqlc.arg('before') LIMIT 5000);
