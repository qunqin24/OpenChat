-- Scheduled tasks (api/tasks.go). Every statement but the scheduler's names the
-- owner, so another person's task reads as missing.

-- name: ListTasks :many
-- A person's tasks whose conversation they can still open, newest first.
SELECT t.*
FROM scheduled_tasks t
         JOIN conversations c ON c.id = t.conversation_id
WHERE t.user_id = sqlc.arg('user_id')
  AND c.deleted_at IS NULL
ORDER BY t.created_at DESC, t.id DESC;

-- name: GetTask :one
SELECT t.*
FROM scheduled_tasks t
         JOIN conversations c ON c.id = t.conversation_id
WHERE t.id = sqlc.arg('id')
  AND t.user_id = sqlc.arg('user_id')
  AND c.deleted_at IS NULL;

-- name: LockTask :one
-- GetTask for an update: the row is read again under its lock, so a change
-- the scheduler or another request made meanwhile is kept, not written over.
SELECT t.*
FROM scheduled_tasks t
         JOIN conversations c ON c.id = t.conversation_id
WHERE t.id = sqlc.arg('id')
  AND t.user_id = sqlc.arg('user_id')
  AND c.deleted_at IS NULL
    FOR UPDATE OF t;

-- name: CountEnabledTasks :one
-- What the per-person limit counts: tasks still to run.
SELECT count(*)::bigint
FROM scheduled_tasks t
         JOIN conversations c ON c.id = t.conversation_id
WHERE t.user_id = sqlc.arg('user_id')
  AND t.enabled
  AND t.next_run_at IS NOT NULL
  AND c.deleted_at IS NULL;

-- name: LockTaskOwner :exec
-- Held while a task is created or switched on, so two requests at once cannot
-- both find room under the limit.
SELECT pg_advisory_xact_lock(hashtextextended('scheduled_tasks:' || sqlc.arg('user_id')::uuid::text, 0));

-- name: CreateTask :one
INSERT INTO scheduled_tasks (user_id, conversation_id, title, prompt, prompt_checked, kind, run_at, minute_of_day,
                             weekdays, month_day, time_zone, enabled, next_run_at)
VALUES (sqlc.arg('user_id'), sqlc.arg('conversation_id'), sqlc.arg('title'), sqlc.arg('prompt'),
        sqlc.narg('prompt_checked'), sqlc.arg('kind'), sqlc.narg('run_at'), sqlc.narg('minute_of_day'),
        sqlc.arg('weekdays'), sqlc.narg('month_day'), sqlc.arg('time_zone'), sqlc.arg('enabled'),
        sqlc.narg('next_run_at'))
RETURNING *;

-- name: UpdateTask :one
-- Everything a person can change at once; the caller fills in what stays.
UPDATE scheduled_tasks
SET title          = sqlc.arg('title'),
    prompt         = sqlc.arg('prompt'),
    prompt_checked = sqlc.narg('prompt_checked'),
    kind           = sqlc.arg('kind'),
    run_at         = sqlc.narg('run_at'),
    minute_of_day  = sqlc.narg('minute_of_day'),
    weekdays       = sqlc.arg('weekdays'),
    month_day      = sqlc.narg('month_day'),
    time_zone      = sqlc.arg('time_zone'),
    enabled        = sqlc.arg('enabled'),
    next_run_at    = sqlc.narg('next_run_at'),
    last_error     = CASE WHEN sqlc.arg('clear_error')::bool THEN NULL ELSE last_error END,
    updated_at     = now()
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id')
RETURNING *;

-- name: DeleteTask :execrows
DELETE FROM scheduled_tasks
WHERE id = sqlc.arg('id')
  AND user_id = sqlc.arg('user_id');

-- name: ClaimDueTasks :many
-- The scheduler's pick: tasks whose time has come, locked so that another
-- server's pass skips them. The caller moves each one's next_run_at on in the
-- same transaction, so it is taken once.
SELECT *
FROM scheduled_tasks
WHERE enabled
  AND next_run_at IS NOT NULL
  AND next_run_at <= sqlc.arg('now')
ORDER BY next_run_at
LIMIT sqlc.arg('row_limit')
    FOR UPDATE SKIP LOCKED;

-- name: AdvanceTask :exec
-- A due task's turn is taken: when it runs next (NULL: a one-off that has now
-- had its turn). The run it starts, or why none, is recorded after.
UPDATE scheduled_tasks
SET next_run_at = sqlc.narg('next_run_at'),
    last_run_at = sqlc.arg('ran_at'),
    updated_at  = now()
WHERE id = sqlc.arg('id');

-- name: TaskStarted :exec
UPDATE scheduled_tasks
SET last_run_at = now(),
    last_run_id = sqlc.arg('run_id'),
    last_error  = NULL,
    updated_at  = now()
WHERE id = sqlc.arg('id');

-- name: TaskFailed :exec
-- Why a turn started nothing. `pause` stops the task: a prompt content
-- moderation refuses would be refused every time.
UPDATE scheduled_tasks
SET last_error  = sqlc.arg('last_error'),
    enabled     = enabled AND NOT sqlc.arg('pause')::bool,
    next_run_at = CASE WHEN sqlc.arg('pause')::bool THEN NULL ELSE next_run_at END,
    updated_at  = now()
WHERE id = sqlc.arg('id');

-- name: SetTaskPromptChecked :exec
UPDATE scheduled_tasks
SET prompt_checked = sqlc.arg('prompt_checked')
WHERE id = sqlc.arg('id')
  AND prompt = sqlc.arg('prompt');

-- name: SetUserTimeZone :exec
UPDATE users
SET time_zone = sqlc.arg('time_zone')
WHERE id = sqlc.arg('id');

-- name: TaskToolContext :one
-- What decides whether a run is offered the task tool, and the times it reads:
-- the conversation's owner, outside a temporary conversation.
SELECT u.time_zone, c.temporary, c.model
FROM conversations c
         JOIN users u ON u.id = c.user_id
WHERE c.id = sqlc.arg('conversation_id')
  AND c.user_id = sqlc.arg('user_id');
