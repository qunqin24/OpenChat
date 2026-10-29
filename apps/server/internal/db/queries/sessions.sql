-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at, user_agent)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetSessionUser :one
-- One round trip for the whole auth check: the session must be unexpired and
-- the account still enabled and not waiting for approval. A pending account is
-- never given a session; this is the second lock on that door, for the day a
-- code path forgets the first.
SELECT sqlc.embed(users), s.id AS session_id, s.expires_at
FROM sessions s
         JOIN users ON users.id = s.user_id
WHERE s.token_hash = sqlc.arg('token_hash')
  AND s.expires_at > now()
  AND users.disabled_at IS NULL
  AND NOT users.pending;

-- name: GetDisabledSessionUser :one
-- A live session whose account has been disabled since: it is kept, so the
-- person is shown why (auth.Middleware) rather than signed out, and can do
-- nothing else with it. Never a pending account's.
SELECT sqlc.embed(users)
FROM sessions s
         JOIN users ON users.id = s.user_id
WHERE s.token_hash = sqlc.arg('token_hash')
  AND s.expires_at > now()
  AND users.disabled_at IS NOT NULL
  AND NOT users.pending;

-- name: TouchSession :exec
UPDATE sessions
SET last_used_at = now()
WHERE id = $1;

-- name: DeleteSession :exec
DELETE
FROM sessions
WHERE token_hash = sqlc.arg('token_hash');

-- name: DeleteUserSessions :exec
DELETE
FROM sessions
WHERE user_id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE
FROM sessions
WHERE expires_at <= now();

-- name: DeleteSessionsForUsers :exec
DELETE
FROM sessions
WHERE user_id = ANY (sqlc.arg('user_ids')::uuid[]);

-- name: DeleteOtherUserSessions :exec
-- Used when someone changes their own password: every session issued under the
-- old credential goes, except the one asking, so the change does not sign the
-- person out of the tab they are standing in.
DELETE
FROM sessions
WHERE user_id = sqlc.arg('user_id')
  AND id <> sqlc.arg('keep_id');
