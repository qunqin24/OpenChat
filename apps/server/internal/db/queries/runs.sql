-- name: CreateRun :one
INSERT INTO runs (conversation_id, user_id, input_message_id, request_id, model_id, channel_id, reasoning_effort,
                  web_search, continues, client_ip)
VALUES (sqlc.arg('conversation_id'), sqlc.arg('user_id'), sqlc.arg('input_message_id'),
        sqlc.narg('request_id'), sqlc.arg('model_id'), sqlc.narg('channel_id'), sqlc.narg('reasoning_effort'),
        sqlc.arg('web_search'), sqlc.narg('continues'), sqlc.narg('client_ip'))
RETURNING *;

-- name: LockRunSlots :exec
-- Held until the transaction ends, so that counting the runs that are going
-- and then starting one is a single step: two requests with one slot left
-- cannot both be let through. The key is the account's id, or for a guest the
-- address (`ip:...`); servers sharing the database share the lock.
SELECT pg_advisory_xact_lock(hashtextextended('run_slots:' || sqlc.arg('key')::text, 23));

-- name: CountUserRunningRuns :one
-- How many runs of this account are going, in all its conversations.
SELECT count(*)::bigint
FROM runs
WHERE user_id = $1
  AND status = 'running';

-- name: CountIPRunningRuns :one
-- How many guest runs from this address are going. A guest's run lives inside
-- the request that follows it and is never resumed after a restart, so one that
-- has committed nothing for a long while is a leftover of a process that died
-- (the sweep deletes its user within hours), not a run that is going: it does
-- not hold a slot.
SELECT count(*)::bigint
FROM runs
WHERE client_ip = sqlc.arg('ip')::text
  AND status = 'running'
  AND updated_at > now() - interval '20 minutes';

-- name: LockRun :one
-- Held until the transaction ends, so two answers to one question queue up and
-- the second finds the first's.
SELECT *
FROM runs
WHERE id = $1
FOR UPDATE;

-- name: GetRun :one
SELECT *
FROM runs
WHERE id = $1;

-- name: GetRunByRequestID :one
SELECT *
FROM runs
WHERE conversation_id = sqlc.arg('conversation_id')
  AND request_id = sqlc.arg('request_id');

-- name: GetActiveRun :one
SELECT *
FROM runs
WHERE conversation_id = $1
  AND status = 'running';

-- name: ListRunningRuns :many
-- What a process that just started has to pick up. A guest's run is not one:
-- the request that was following it is gone with the process that died, and
-- the sweep for guests deletes what is left (guest.sql).
SELECT runs.*
FROM runs
         JOIN users ON users.id = runs.user_id
WHERE runs.status = 'running'
  AND NOT users.guest
ORDER BY runs.created_at;

-- name: SaveRunState :one
-- One step of a run: a new checkpoint, new live state, or both. Guarded on
-- `running` so nothing can resurrect a run that already finished.
UPDATE runs
SET checkpoint = sqlc.arg('checkpoint'),
    live       = sqlc.arg('live'),
    version    = version + 1,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status = 'running'
RETURNING *;

-- name: MoveRunChannel :exec
-- Fails a run over to another channel of its model (agent failover): the
-- request that failed produced nothing, so the next one is made there instead.
-- Guarded on `running` like every other step.
UPDATE runs
SET channel_id = sqlc.arg('channel_id'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status = 'running';

-- name: FinishRun :one
-- Terminal. Live state is cleared in the same statement: whatever it held has
-- already been written as messages by the commit that calls this.
UPDATE runs
SET status      = sqlc.arg('status'),
    error_code  = sqlc.narg('error_code'),
    live        = '{}'::jsonb,
    version     = version + 1,
    updated_at  = now(),
    finished_at = now()
WHERE id = sqlc.arg('id')
  AND status = 'running'
RETURNING *;

-- name: RequestRunCancel :one
UPDATE runs
SET cancel_requested = true,
    updated_at       = now()
WHERE id = sqlc.arg('id')
  AND conversation_id = sqlc.arg('conversation_id')
  AND status = 'running'
RETURNING *;
