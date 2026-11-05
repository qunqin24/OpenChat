-- name: LockImageQuota :exec
-- Serialises one person's image requests against their daily allowance, for
-- the transaction that counts and takes a share of it.
SELECT pg_advisory_xact_lock(hashtextextended('image_quota:' || sqlc.arg('user_id')::uuid::text, 0));

-- name: ImagesSince :one
-- How many pictures a person's requests have taken since a moment: the ones
-- under way and the ones done, not the ones that failed.
SELECT COALESCE(sum(count), 0)::bigint
FROM image_requests
WHERE user_id = sqlc.arg('user_id')
  AND created_at >= sqlc.arg('since')
  AND status <> 'failed';

-- name: InsertImageRequest :one
INSERT INTO image_requests (user_id, run_id, count)
VALUES (sqlc.arg('user_id'), sqlc.narg('run_id'), sqlc.arg('count'))
RETURNING id;

-- name: FinishImageRequest :exec
-- A request done keeps what it made (it may be fewer than it asked for); a
-- failed one gives its share back.
UPDATE image_requests
SET status = sqlc.arg('status'),
    count  = GREATEST(sqlc.arg('count')::int, 1)
WHERE id = sqlc.arg('id');

-- name: UsersImagesSince :many
-- ImagesSince for a page of accounts at once, for the users list.
SELECT user_id, COALESCE(sum(count), 0)::bigint AS images
FROM image_requests
WHERE user_id = ANY (sqlc.arg('user_ids')::uuid[])
  AND created_at >= sqlc.arg('since')
  AND status <> 'failed'
GROUP BY user_id;
