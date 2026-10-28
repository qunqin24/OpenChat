-- +goose Up
-- +goose StatementBegin
-- Sub-agents (子智能体, docs/design/sub-agents.md): the main agent hands a
-- self-contained piece of work to a sub-agent, which does it in a clean
-- context of its own and hands back only its report.
--
-- A sub-agent is a hidden conversation of its own (kind 'subagent'), a child
-- of the conversation whose run launched it, and each stretch of its work is
-- an ordinary run there: resumable, streamed, metered and cancellable like any
-- other. Everything that lists conversations to a person - the sidebar,
-- search, the library, exports, titles - reads only kind 'chat'.
ALTER TABLE conversations
    ADD COLUMN kind            text    NOT NULL DEFAULT 'chat',
    -- The conversation the sub-agent works for. Its owner is the child's
    -- owner; deleting it deletes the child.
    ADD COLUMN parent_id       uuid REFERENCES conversations (id) ON DELETE CASCADE,
    -- The run that launched it. When that answer goes - its question edited,
    -- the answer regenerated - the sub-agent goes with it, as the answer's
    -- writing blocks do.
    ADD COLUMN launched_by     uuid REFERENCES runs (id) ON DELETE CASCADE,
    -- The tool call that launched it: a call replayed after a restart finds
    -- the sub-agent it already started instead of starting another.
    ADD COLUMN launch_call_id  text,
    -- Whether its reports are delivered to the conversation (background) or
    -- taken by the tool call waiting for it (foreground). A foreground one
    -- moved to the background ("转到后台"), and every one a message continues,
    -- is background from then on.
    ADD COLUMN background      boolean NOT NULL DEFAULT false,
    -- Stopped by the person: the main agent's messages do not wake it, only
    -- the person's "继续" does.
    ADD COLUMN stopped_by_user boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT conversations_kind CHECK (kind IN ('chat', 'subagent')),
    ADD CONSTRAINT conversations_subagent_parent CHECK ((kind = 'subagent') = (parent_id IS NOT NULL));

CREATE INDEX conversations_parent_idx ON conversations (parent_id, created_at) WHERE parent_id IS NOT NULL;
CREATE UNIQUE INDEX conversations_launch_idx ON conversations (launched_by, launch_call_id) WHERE launched_by IS NOT NULL;

-- Who wrote a user row that no person typed: 'agent' is the main agent's task
-- or message to a sub-agent, 'subagent' a sub-agent's report delivered to the
-- main conversation. NULL is everything else. Neither is moderated (a model
-- wrote both), neither is a question the person asked.
ALTER TABLE messages
    ADD COLUMN origin text,
    ADD CONSTRAINT messages_origin CHECK (origin IN ('agent', 'subagent'));

-- One report per run of a sub-agent, written in the transaction that ends the
-- run: a continued sub-agent is a new run and so a new report, never mistaken
-- for the first one. Pending until it is delivered - as a row of the main
-- conversation (delivered_message_id), or as the result of the foreground
-- call that waited for it (via_tool). A delivered row deleted later (the
-- answer that received it regenerated) puts the report back to pending.
CREATE TABLE subagent_reports (
    run_id               uuid PRIMARY KEY REFERENCES runs (id) ON DELETE CASCADE,
    conversation_id      uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    parent_id            uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    state                text        NOT NULL,
    report               text        NOT NULL,
    duration_ms          bigint      NOT NULL,
    via_tool             boolean     NOT NULL DEFAULT false,
    delivered_message_id uuid REFERENCES messages (id) ON DELETE SET NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT subagent_reports_state CHECK (state IN ('completed', 'failed', 'cancelled'))
);
CREATE INDEX subagent_reports_pending_idx ON subagent_reports (parent_id, created_at)
    WHERE delivered_message_id IS NULL AND NOT via_tool;
CREATE INDEX subagent_reports_conversation_idx ON subagent_reports (conversation_id, created_at);
CREATE INDEX subagent_reports_delivered_idx ON subagent_reports (delivered_message_id)
    WHERE delivered_message_id IS NOT NULL;

-- Messages from the main agent to a sub-agent that is working: its run writes
-- them into its transcript before its next model request. One that arrives as
-- the run ends starts the next run instead.
CREATE TABLE subagent_inbox (
    id              uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    content         text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX subagent_inbox_conversation_idx ON subagent_inbox (conversation_id, created_at);

ALTER TABLE usage_logs
    DROP CONSTRAINT usage_logs_kind;
ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_kind CHECK (kind IN ('chat', 'title', 'compression', 'moderation', 'subagent'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM usage_logs WHERE kind = 'subagent';
ALTER TABLE usage_logs
    DROP CONSTRAINT usage_logs_kind;
ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_kind CHECK (kind IN ('chat', 'title', 'compression', 'moderation'));
DROP TABLE subagent_inbox;
DROP TABLE subagent_reports;
DELETE FROM messages WHERE origin IS NOT NULL;
ALTER TABLE messages
    DROP CONSTRAINT messages_origin,
    DROP COLUMN origin;
DELETE FROM conversations WHERE kind = 'subagent';
DROP INDEX conversations_launch_idx;
DROP INDEX conversations_parent_idx;
ALTER TABLE conversations
    DROP CONSTRAINT conversations_subagent_parent,
    DROP CONSTRAINT conversations_kind,
    DROP COLUMN stopped_by_user,
    DROP COLUMN background,
    DROP COLUMN launch_call_id,
    DROP COLUMN launched_by,
    DROP COLUMN parent_id,
    DROP COLUMN kind;
-- +goose StatementEnd
