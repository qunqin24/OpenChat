-- +goose Up
-- +goose StatementBegin
-- Calling plugin tools from conversations (docs/design/plugins-plan.md §6):
-- the model writes a script that calls them (plugin_code), or, for a plugin
-- set to list its tools directly, calls them one at a time.

-- A plugin whose tools are offered to the model one by one instead of through
-- scripts: for a service that is a code mode itself, or a model that writes
-- scripts badly.
ALTER TABLE plugins ADD COLUMN direct_tools boolean NOT NULL DEFAULT false;

-- The plugin catalogue as a conversation's system prompt lists it, taken at
-- its first request that offers plugins, so the prompt keeps its bytes as
-- people install plugins or administrators refresh tools. `plugins` are the
-- plugins it listed, by id, to tell the model what changed since.
ALTER TABLE conversations ADD COLUMN plugin_catalog jsonb;

-- A call that waits for the person to allow it. The script stays paused in
-- its run meanwhile; the row is what the answer is checked against, and what
-- a page that reconnects shows.
CREATE TABLE plugin_confirmations (
    id              uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    run_id          uuid        NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The plugin_code call (or the direct call) it belongs to, and which of
    -- the script's calls it is.
    call_id         text        NOT NULL,
    seq             integer     NOT NULL,
    plugin_id       uuid        NOT NULL REFERENCES plugins (id) ON DELETE CASCADE,
    tool            text        NOT NULL,
    arguments       jsonb       NOT NULL,
    status          text        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'allowed', 'denied', 'expired')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    answered_at     timestamptz
);
CREATE INDEX plugin_confirmations_run ON plugin_confirmations (run_id);

-- "Allow this tool for the rest of this conversation".
CREATE TABLE plugin_tool_grants (
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    plugin_id       uuid        NOT NULL REFERENCES plugins (id) ON DELETE CASCADE,
    tool            text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, plugin_id, tool)
);

-- One row per tool call, for the administrators: who, which tool, how it
-- ended. Never the arguments or the result. Kept when the person, the
-- conversation or the plugin goes.
CREATE TABLE plugin_calls (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id         uuid REFERENCES users (id) ON DELETE SET NULL,
    conversation_id uuid REFERENCES conversations (id) ON DELETE SET NULL,
    plugin_id       uuid REFERENCES plugins (id) ON DELETE SET NULL,
    plugin_slug     text        NOT NULL,
    tool            text        NOT NULL,
    ok              boolean     NOT NULL,
    -- How it ended when not ok: denied, not_connected, invalid, failed...
    outcome         text        NOT NULL DEFAULT '',
    confirmed       boolean     NOT NULL DEFAULT false,
    duration_ms     integer     NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX plugin_calls_created ON plugin_calls (created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE plugin_calls;
DROP TABLE plugin_tool_grants;
DROP TABLE plugin_confirmations;
ALTER TABLE conversations DROP COLUMN plugin_catalog;
ALTER TABLE plugins DROP COLUMN direct_tools;
-- +goose StatementEnd
