-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at, user_agent)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetSessionUser :one
-- One round trip for the whole auth check: the session must be unexpired and
-- the account still enabled.
SELECT sqlc.embed(users), s.id AS session_id, s.expires_at
FROM sessions s
         JOIN users ON users.id = s.user_id
WHERE s.token_hash = sqlc.arg('token_hash')
  AND s.expires_at > now()
  AND users.disabled_at IS NULL;

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
