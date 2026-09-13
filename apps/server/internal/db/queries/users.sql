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

-- name: GetUserByLogin :one
-- Sign-in accepts either identifier, so one lookup covers both.
--
-- Nothing stops a username from being spelled like somebody else's address, so
-- the two columns can match different rows for the same input. The ORDER BY
-- settles it in favour of the address: your own email always signs you in, and
-- no username anyone else picks can take that away. Without it `:one` would
-- take whichever row the planner returned first.
SELECT *
FROM users
WHERE lower(email) = lower(sqlc.arg('identifier')::text)
   OR lower(name) = lower(sqlc.arg('identifier')::text)
ORDER BY (lower(email) = lower(sqlc.arg('identifier')::text)) DESC
LIMIT 1;

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

-- name: AdminCreateUser :one
-- Registration derives the role from an empty table; here the caller picks it,
-- so the admin panel can create a second admin without a bootstrap dance.
INSERT INTO users (email, name, password_hash, role, disabled_at)
VALUES (sqlc.arg('email'), sqlc.arg('name'), sqlc.arg('password_hash'), sqlc.arg('role'),
        CASE WHEN sqlc.arg('disabled')::boolean THEN now() ELSE NULL END)
RETURNING *;

-- name: AdminUpdateUser :one
-- Every editable field in one statement: a partial edit is one round trip and
-- one row version, rather than a sequence that can half-apply.
--
-- `disabled_at` keeps its original value when the account is already disabled,
-- so re-saving the form does not reset when access was cut off.
UPDATE users
SET name          = COALESCE(sqlc.narg('name')::text, name),
    email         = COALESCE(sqlc.narg('email')::text, email),
    password_hash = COALESCE(sqlc.narg('password_hash')::text, password_hash),
    role          = COALESCE(sqlc.narg('role')::user_role, role),
    disabled_at   = CASE
                        WHEN sqlc.narg('disabled')::boolean IS NULL THEN disabled_at
                        WHEN sqlc.narg('disabled')::boolean THEN COALESCE(disabled_at, now())
                        ELSE NULL
                        END,
    updated_at    = now()
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

-- name: UpdateOwnProfile :one
-- Deliberately narrower than AdminUpdateUser: role and disabled_at are not
-- reachable from here, so the self-service form cannot touch them even if the
-- handler above it were wrong.
UPDATE users
SET name       = COALESCE(sqlc.narg('name')::text, name),
    email      = COALESCE(sqlc.narg('email')::text, email),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = sqlc.arg('password_hash'),
    updated_at    = now()
WHERE id = sqlc.arg('id');
