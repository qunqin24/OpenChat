-- name: ListChannels :many
SELECT *
FROM channels
ORDER BY sort_order, created_at;

-- name: GetChannel :one
SELECT *
FROM channels
WHERE id = $1;

-- name: CreateChannel :one
-- An empty format is the default, so a caller that has no opinion — tests,
-- older code — gets Chat Completions rather than a constraint violation.
INSERT INTO channels (name, type, base_url, api_key, enabled, api_format, send_user_id)
VALUES ($1, $2, $3, $4, $5, COALESCE(NULLIF(sqlc.arg('api_format')::text, ''), 'chat'), sqlc.arg('send_user_id')::boolean)
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
    sort_order = COALESCE(sqlc.narg('sort_order')::integer, sort_order),
    api_format = COALESCE(sqlc.narg('api_format')::text, api_format),
    send_user_id = COALESCE(sqlc.narg('send_user_id')::boolean, send_user_id),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteChannel :execrows
DELETE
FROM channels
WHERE id = $1;
