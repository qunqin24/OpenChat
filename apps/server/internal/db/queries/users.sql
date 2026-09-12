-- name: RegisterUser :one
-- The first account created becomes the admin: someone has to be able to reach
-- the admin panel, and there is no other bootstrap path. The decision is made
-- inside the INSERT so two concurrent registrations cannot both see an empty
-- table and both claim it.
INSERT INTO users (email, name, password_hash, role)
VALUES ($1, $2, $3,
        CASE WHEN (SELECT count(*) FROM users) = 0
                 THEN 'admin'::user_role
             ELSE 'user'::user_role END)
RETURNING *;

-- name: GetUser :one
SELECT *
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT *
FROM users
WHERE lower(email) = lower(sqlc.arg('email')::text);

-- name: CountUsers :one
SELECT count(*)
FROM users;

-- name: TouchUserSeen :exec
UPDATE users
SET last_seen_at = now()
WHERE id = $1;

-- name: ListUsers :many
-- Offset paging rather than keyset: the admin table needs page numbers and a
-- total, and the row count here stays small enough that the scan is cheap.
SELECT *
FROM users
WHERE (sqlc.narg('search')::text IS NULL
    OR email ILIKE '%' || sqlc.narg('search')::text || '%'
    OR name ILIKE '%' || sqlc.narg('search')::text || '%')
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: CountUsersFiltered :one
SELECT count(*)
FROM users
WHERE (sqlc.narg('search')::text IS NULL
    OR email ILIKE '%' || sqlc.narg('search')::text || '%'
    OR name ILIKE '%' || sqlc.narg('search')::text || '%');

-- name: UpdateUserRole :one
UPDATE users
SET role       = sqlc.arg('role'),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: SetUserDisabled :one
UPDATE users
SET disabled_at = CASE WHEN sqlc.arg('disabled')::boolean THEN now() ELSE NULL END,
    updated_at  = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: CountAdmins :one
SELECT count(*)
FROM users
WHERE role = 'admin'
  AND disabled_at IS NULL;

-- name: ListUsersByIDs :many
SELECT *
FROM users
WHERE id = ANY (sqlc.arg('ids')::uuid[]);

-- name: BulkUpdateUserRole :execrows
UPDATE users
SET role       = sqlc.arg('role'),
    updated_at = now()
WHERE id = ANY (sqlc.arg('ids')::uuid[]);

-- name: BulkSetUserDisabled :execrows
UPDATE users
SET disabled_at = CASE WHEN sqlc.arg('disabled')::boolean THEN now() ELSE NULL END,
    updated_at  = now()
WHERE id = ANY (sqlc.arg('ids')::uuid[]);
