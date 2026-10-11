-- Content moderation (api/moderation.go): the log of what was turned away, the
-- strikes counted from it, and the guest addresses banned for them.

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
INSERT INTO moderation_events (user_id, guest_ip, conversation_id, source, mode, categories, excerpt)
VALUES (sqlc.narg('user_id'), sqlc.narg('guest_ip'), sqlc.narg('conversation_id'), sqlc.arg('source'),
        sqlc.arg('mode'), sqlc.arg('categories')::text[], sqlc.arg('excerpt'))
RETURNING *;

-- name: CountUserStrikes :one
-- An account's blocked messages in the window, counted from the last time it
-- was sanctioned: an administrator who enables the account again gives it a
-- fresh start, instead of the next message setting the same punishment off.
SELECT count(*)::bigint
FROM moderation_events e
WHERE e.user_id = sqlc.arg('user_id')
  AND e.created_at > now() - make_interval(hours => sqlc.arg('window_hours')::int)
  AND e.created_at > COALESCE((SELECT max(s.created_at)
                               FROM moderation_events s
                               WHERE s.user_id = sqlc.arg('user_id')
                                 AND s.sanction IS NOT NULL), '-infinity'::timestamptz);

-- name: CountGuestStrikes :one
-- The same for a guest address.
SELECT count(*)::bigint
FROM moderation_events e
WHERE e.guest_ip = sqlc.arg('guest_ip')
  AND e.created_at > now() - make_interval(hours => sqlc.arg('window_hours')::int)
  AND e.created_at > COALESCE((SELECT max(s.created_at)
                               FROM moderation_events s
                               WHERE s.guest_ip = sqlc.arg('guest_ip')
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
-- moderation_events_created_idx. The account's name and whether it is disabled,
-- and whether a guest's address is banned now, are read alongside.
SELECT e.id,
       e.created_at,
       e.user_id,
       u.name                                       AS user_name,
       u.email                                      AS user_email,
       (u.disabled_at IS NOT NULL)::boolean         AS user_disabled,
       e.guest_ip,
       (b.ip IS NOT NULL)::boolean                  AS ip_banned,
       e.conversation_id,
       e.source,
       e.mode,
       e.categories,
       e.excerpt,
       e.sanction
FROM moderation_events e
         LEFT JOIN users u ON u.id = e.user_id
         LEFT JOIN guest_bans b ON b.ip = e.guest_ip AND (b.expires_at IS NULL OR b.expires_at > now())
WHERE (sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (e.created_at, e.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
  AND (sqlc.narg('user_id')::uuid IS NULL OR e.user_id = sqlc.narg('user_id')::uuid)
  AND (sqlc.narg('guest_ip')::text IS NULL OR e.guest_ip = sqlc.narg('guest_ip')::text)
  AND (sqlc.narg('category')::text IS NULL OR e.categories @> ARRAY [sqlc.narg('category')::text])
ORDER BY e.created_at DESC, e.id DESC
LIMIT sqlc.arg('row_limit');

-- name: PurgeModerationEvents :execrows
DELETE
FROM moderation_events
WHERE id IN (SELECT m.id FROM moderation_events m WHERE m.created_at < sqlc.arg('before') LIMIT 5000);

-- name: GetGuestBan :one
-- The ban in force on an address: a ban past its expiry is ignored whether or
-- not the cleanup has removed it yet.
SELECT *
FROM guest_bans
WHERE ip = $1
  AND (expires_at IS NULL OR expires_at > now());

-- name: UpsertGuestBan :one
-- A ban on an address already banned lengthens it, never shortens: the later
-- expiry stands, and a permanent ban stays permanent.
INSERT INTO guest_bans (ip, expires_at, reason, event_id)
VALUES (sqlc.arg('ip'), sqlc.narg('expires_at'), sqlc.arg('reason'), sqlc.narg('event_id'))
ON CONFLICT (ip) DO UPDATE
    SET expires_at = CASE
                         WHEN guest_bans.expires_at IS NULL OR excluded.expires_at IS NULL THEN NULL
                         ELSE GREATEST(guest_bans.expires_at, excluded.expires_at)
        END,
        created_at = CASE
                         WHEN guest_bans.expires_at IS NOT NULL AND guest_bans.expires_at <= now() THEN now()
                         ELSE guest_bans.created_at
            END,
        reason     = excluded.reason,
        event_id   = excluded.event_id
RETURNING *;

-- name: DeleteGuestBan :execrows
DELETE
FROM guest_bans
WHERE ip = $1;

-- name: ListGuestBans :many
-- The bans in force, newest first.
SELECT *
FROM guest_bans
WHERE expires_at IS NULL
   OR expires_at > now()
ORDER BY created_at DESC, ip
LIMIT 500;

-- name: PurgeExpiredGuestBans :execrows
DELETE
FROM guest_bans
WHERE expires_at IS NOT NULL
  AND expires_at <= now();
