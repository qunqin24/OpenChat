-- Limits on signing in and signing up (api/auth_limits.go). One row per attempt
-- that counts; see the migration for the columns.

-- name: LockAuthAttempts :exec
-- Held until the transaction ends, so that counting and then recording an
-- attempt is one step: concurrent attempts at the limit cannot all be let
-- through, and servers sharing the database share the count.
SELECT pg_advisory_xact_lock(hashtextextended('auth_attempts', 12));

-- name: AuthAttemptsFull :one
-- Whether this address (and, when a subject is given, this subject from it) has
-- used up `row_limit` attempts of this kind in the window - and if so, in how
-- many seconds the count falls below the limit again: when the row_limit-th
-- most recent attempt leaves the window. No row: not at the limit.
SELECT GREATEST(1, ceil(extract(epoch FROM (a.created_at + make_interval(secs => sqlc.arg('window_seconds')::double precision) - now()))))::int AS retry_after
FROM auth_attempts a
WHERE a.kind = sqlc.arg('kind')::text
  AND a.ip = sqlc.arg('ip')::text
  AND (sqlc.narg('subject')::text IS NULL OR a.subject = sqlc.narg('subject')::text)
  AND a.created_at > now() - make_interval(secs => sqlc.arg('window_seconds')::double precision)
ORDER BY a.created_at DESC
OFFSET sqlc.arg('row_limit')::int - 1 LIMIT 1;

-- name: InsertAuthAttempt :one
INSERT INTO auth_attempts (kind, ip, subject)
VALUES (sqlc.arg('kind'), sqlc.arg('ip'), sqlc.arg('subject'))
RETURNING id;

-- name: DeleteAuthAttempt :exec
-- A sign-in whose password turned out right gives its slot back: only failures
-- count.
DELETE
FROM auth_attempts
WHERE id = $1;

-- name: PurgeAuthAttempts :execrows
-- Nothing counts an attempt for longer than the longest window.
DELETE
FROM auth_attempts
WHERE created_at < sqlc.arg('before');
