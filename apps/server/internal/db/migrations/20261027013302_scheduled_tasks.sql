-- +goose Up
-- +goose StatementBegin
-- Scheduled tasks: an instruction that is sent, at the times its schedule
-- names, as a new message in the task's own conversation, and answered there
-- as if the person had sent it (api/tasks.go, the scheduler in
-- api/task_scheduler.go).
CREATE TABLE scheduled_tasks (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The conversation the runs go into. Deleting it for good takes the task.
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    title           text        NOT NULL,
    prompt          text        NOT NULL,
    -- The hash of the prompt that content moderation passed (memory.Hash); a
    -- prompt not checked as it is now is checked before it is sent.
    prompt_checked  text,
    -- once: at run_at. daily / weekly / monthly: at minute_of_day in
    -- time_zone, on weekdays (0 = Sunday) or on month_day (the month's last
    -- day when it has fewer).
    kind            text        NOT NULL,
    run_at          timestamptz,
    minute_of_day   int,
    weekdays        smallint[]  NOT NULL DEFAULT '{}',
    month_day       smallint,
    time_zone       text        NOT NULL,
    enabled         boolean     NOT NULL DEFAULT true,
    -- When it runs next; NULL once a one-off has run, or while paused.
    next_run_at     timestamptz,
    last_run_at     timestamptz,
    last_run_id     uuid        REFERENCES runs (id) ON DELETE SET NULL,
    -- Why the last time it was due did not start a run, as a code to localise.
    last_error      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT scheduled_tasks_kind CHECK (kind IN ('once', 'daily', 'weekly', 'monthly')),
    CONSTRAINT scheduled_tasks_minute CHECK (minute_of_day IS NULL OR minute_of_day BETWEEN 0 AND 1439),
    CONSTRAINT scheduled_tasks_month_day CHECK (month_day IS NULL OR month_day BETWEEN 1 AND 31)
);

CREATE INDEX scheduled_tasks_due_idx ON scheduled_tasks (next_run_at) WHERE enabled AND next_run_at IS NOT NULL;
CREATE INDEX scheduled_tasks_user_idx ON scheduled_tasks (user_id, created_at DESC);

-- Where a person is, for times a model is asked to schedule: the browser says.
ALTER TABLE users ADD COLUMN time_zone text;

-- A task's prompt as it is saved by its owner (`task`, a strike like any text
-- they send) or checked before it is sent, or as a model wrote it (`task_run`,
-- logged but not a strike: the words are not being typed now).
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_source;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_source CHECK (source IN ('send', 'edit', 'answer', 'guest', 'instructions', 'memory', 'snapshot', 'task', 'task_run'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM moderation_events WHERE source IN ('task', 'task_run');
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_source;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_source CHECK (source IN ('send', 'edit', 'answer', 'guest', 'instructions', 'memory', 'snapshot'));
ALTER TABLE users DROP COLUMN time_zone;
DROP TABLE scheduled_tasks;
-- +goose StatementEnd
