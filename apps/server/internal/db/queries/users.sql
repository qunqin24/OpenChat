-- name: LockRegistration :exec
-- Registrations take this before RegisterUser, so that "is the table empty?" is
-- answered one registration at a time: two people registering on a fresh
-- install cannot both be made the administrator.
SELECT pg_advisory_xact_lock(hashtextextended('registration', 13));

-- name: RegisterUser :one
-- The first account created becomes the admin: someone has to be able to reach
-- the admin panel, and there is no other bootstrap path. It is never pending
-- and never refused, whatever the switches say - a fresh install must not be
-- able to lock itself out. Everyone after it gets no row back while
-- registration is closed, and is pending while approval is required. A guest
-- (a user row that exists for one run, see guest.sql) is not an account and
-- does not count. The decision is made inside the INSERT, under the lock
-- LockRegistration takes.
INSERT INTO users (email, name, password_hash, role, pending)
SELECT sqlc.arg('email')::text,
       sqlc.arg('name')::text,
       sqlc.arg('password_hash')::text,
       CASE WHEN c.n = 0 THEN 'admin'::user_role ELSE 'user'::user_role END,
       c.n > 0 AND sqlc.arg('needs_approval')::boolean
FROM (SELECT count(*) AS n FROM users WHERE NOT guest) c
WHERE c.n = 0
   OR sqlc.arg('registration_open')::boolean
RETURNING *;

-- name: HasAccounts :one
-- Whether anyone has registered yet. While nobody has, registration is open to
-- the one account that becomes the administrator, however the switch is set.
SELECT EXISTS (SELECT 1 FROM users WHERE NOT guest) AS has_accounts;

-- name: GetUser :one
SELECT *
FROM users
WHERE id = $1;

-- name: GetUserByLogin :one
-- Sign-in accepts either identifier, so one lookup covers both.
--
-- The `users_name_no_at` constraint keeps usernames and addresses in disjoint
-- name spaces, so the two columns cannot match different rows for one input and
-- the ORDER BY can no longer decide anything. It stays as the guard for if that
-- constraint is ever dropped: without it `:one` would silently take whichever
-- row the planner returned first, which is the wrong account rather than an
-- error. Addresses win, so nobody's username can shadow somebody's email.
SELECT *
FROM users
WHERE NOT guest
  AND (lower(email) = lower(sqlc.arg('identifier')::text)
    OR lower(name) = lower(sqlc.arg('identifier')::text))
ORDER BY (lower(email) = lower(sqlc.arg('identifier')::text)) DESC
LIMIT 1;

-- name: CountUsers :one
SELECT count(*)
FROM users
WHERE NOT guest;

-- name: TouchUserSeen :exec
UPDATE users
SET last_seen_at = now()
WHERE id = $1;

-- name: ListUsers :many
-- Offset paging rather than keyset: the admin table needs page numbers and a
-- total, and the row count here stays small enough that the scan is cheap.
SELECT *
FROM users
WHERE NOT guest
  AND (sqlc.narg('search')::text IS NULL
    OR email ILIKE '%' || sqlc.narg('search')::text || '%'
    OR name ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('statuses')::text[] IS NULL
    OR (CASE
            WHEN disabled_at IS NOT NULL THEN 'disabled'
            WHEN pending THEN 'pending'
            ELSE 'enabled'
        END) = ANY (sqlc.narg('statuses')::text[]))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: CountUsersFiltered :one
SELECT count(*)
FROM users
WHERE NOT guest
  AND (sqlc.narg('search')::text IS NULL
    OR email ILIKE '%' || sqlc.narg('search')::text || '%'
    OR name ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('statuses')::text[] IS NULL
    OR (CASE
            WHEN disabled_at IS NOT NULL THEN 'disabled'
            WHEN pending THEN 'pending'
            ELSE 'enabled'
        END) = ANY (sqlc.narg('statuses')::text[]));

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
--
-- `approved` can only let a pending account in (true); there is no way back to
-- pending. It is independent of `disabled`: enabling an account does not
-- approve it, and approving one does not enable it.
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
    pending       = CASE WHEN sqlc.narg('approved')::boolean IS TRUE THEN false ELSE pending END,
    updated_at    = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: GetContactAdmin :one
-- Who an account that cannot sign in (waiting for approval, or disabled) is
-- told to contact: the longest-standing administrator still enabled - on most
-- instances the one who installed it.
SELECT name, email
FROM users
WHERE role = 'admin'
  AND disabled_at IS NULL
  AND NOT pending
  AND NOT guest
ORDER BY created_at, id
LIMIT 1;

-- name: LockAdminMembership :exec
-- Every change that can take an administrator away (demoting, disabling, one
-- account or a batch) takes this first and holds it until its transaction
-- ends, then counts the enabled admins and changes. Without it, two admins
-- demoting each other both read "2 admins" and both succeed, leaving none.
-- Servers sharing the database share the lock.
SELECT pg_advisory_xact_lock(hashtextextended('admin_membership', 29));

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

-- name: BulkApproveUsers :execrows
-- Only accounts that are waiting count, so the number reported is the number
-- let in.
UPDATE users
SET pending    = false,
    updated_at = now()
WHERE id = ANY (sqlc.arg('ids')::uuid[])
  AND pending;

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

-- name: SetUserAvatar :one
-- The account's picture, or NULL to go back to the initial. Narrower than
-- UpdateOwnProfile on purpose: the picture is its own endpoint, with its own
-- checks, and the profile form has no business with it.
UPDATE users
SET avatar_key = sqlc.narg('avatar_key')::text,
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: GetUserByAvatarKey :one
-- Who a stored picture belongs to: the signed read URL of a locally kept file
-- is served to anyone holding it, and this is what says the key is a picture.
SELECT *
FROM users
WHERE avatar_key = $1;
