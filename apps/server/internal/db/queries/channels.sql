-- name: ListChannels :many
SELECT *
FROM channels
ORDER BY priority DESC, created_at;

-- name: GetChannel :one
SELECT *
FROM channels
WHERE id = $1;

-- name: CreateChannel :one
-- An empty format is the default, so a caller that has no opinion — tests,
-- older code — gets Chat Completions rather than a constraint violation; a zero
-- weight is the default weight, 1, the same way.
INSERT INTO channels (name, type, base_url, api_key, enabled, api_format, send_user_id, priority, weight)
VALUES ($1, $2, $3, $4, $5, COALESCE(NULLIF(sqlc.arg('api_format')::text, ''), 'chat'), sqlc.arg('send_user_id')::boolean,
        sqlc.arg('priority')::integer, COALESCE(NULLIF(sqlc.arg('weight')::integer, 0), 1))
RETURNING *;

-- name: UpdateChannel :one
-- Every editable field in one statement, so a partial edit cannot half-apply.
--
-- `api_key` is the one field whose "absent" and "empty" mean different things:
-- absent keeps the stored credential, empty clears it. A NULL argument is
-- therefore "leave it", which COALESCE gives us for free.
UPDATE channels
SET name       = COALESCE(sqlc.narg('name')::text, name),
    type       = COALESCE(sqlc.narg('type')::channel_type, type),
    base_url   = COALESCE(sqlc.narg('base_url')::text, base_url),
    api_key    = COALESCE(sqlc.narg('api_key')::text, api_key),
    enabled    = COALESCE(sqlc.narg('enabled')::boolean, enabled),
    priority   = COALESCE(sqlc.narg('priority')::integer, priority),
    weight     = COALESCE(sqlc.narg('weight')::integer, weight),
    api_format = COALESCE(sqlc.narg('api_format')::text, api_format),
    send_user_id = COALESCE(sqlc.narg('send_user_id')::boolean, send_user_id),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteChannel :execrows
DELETE
FROM channels
WHERE id = $1;
