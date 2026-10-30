-- name: CreateRun :one
INSERT INTO runs (conversation_id, user_id, input_message_id, request_id, model_id, channel_id, reasoning_effort,
                  web_search, continues, from_app)
VALUES (sqlc.arg('conversation_id'), sqlc.arg('user_id'), sqlc.arg('input_message_id'),
        sqlc.narg('request_id'), sqlc.arg('model_id'), sqlc.narg('channel_id'), sqlc.narg('reasoning_effort'),
        sqlc.arg('web_search'), sqlc.narg('continues'), sqlc.arg('from_app'))
RETURNING *;

-- name: LockRunSlots :exec
-- Held until the transaction ends, so that counting the runs that are going
-- and then starting one is a single step: two requests with one slot left
-- cannot both be let through. The key is the account's id; servers sharing
-- the database share the lock.
SELECT pg_advisory_xact_lock(hashtextextended('run_slots:' || sqlc.arg('key')::text, 23));

-- name: CountUserRunningRuns :one
-- How many runs of this account are going, in all its conversations. A
-- sub-agent's runs are counted apart (CountUserSubagentRuns): two of them
-- working in the background must not keep the person from sending.
SELECT count(*)::bigint
FROM runs
         JOIN conversations c ON c.id = runs.conversation_id
WHERE runs.user_id = $1
  AND runs.status = 'running'
  AND c.kind = 'chat';

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
-- What a process that just started has to pick up.
SELECT *
FROM runs
WHERE status = 'running'
ORDER BY created_at;

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
