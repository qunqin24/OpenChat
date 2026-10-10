-- +goose Up
-- +goose StatementBegin

-- The agent layer's storage, modelled on pi-durable's rule: only committed state
-- is observable. Everything a browser sees about a reply was written here first,
-- so a reload, a second tab or a restarted process all read the same thing.
--
-- `messages` becomes the transcript: one immutable row per user input, per model
-- response and per tool result. A reply that called two tools and then answered
-- is four rows — assistant (calls), tool, tool, assistant (answer) — sharing one
-- run_id. Nothing about a row changes after it is written; the reply still being
-- generated lives in `runs.live` until it is finished and becomes a row.

ALTER TABLE messages
    -- The ordered content blocks: text, reasoning, tool_call, tool_result.
    -- `content` stays as the plain text of the text blocks, for titles, search
    -- and anything that only wants words.
    ADD COLUMN parts       jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- Assistant rows only. Why the response ended: stop, length, tool_calls,
    -- content_filter, cancelled or error. Error and cancelled rows are shown but
    -- never sent back to a model — a half answer or an apology for a failure is
    -- not context the next request should build on.
    ADD COLUMN stop_reason text,
    -- The model and the endpoint that answered, as they reported it.
    ADD COLUMN model       text,
    ADD COLUMN provider    text,
    ADD COLUMN usage       jsonb,
    -- A machine code for a failed response (`model_error`, `no_model`, …). The
    -- provider's own text stays in the server log; it is not localised and may
    -- carry details a user should not see.
    ADD COLUMN error_code  text;

-- Every existing reply was plain text.
UPDATE messages
SET parts = jsonb_build_array(jsonb_build_object('type', 'text', 'text', content))
WHERE content <> '';

-- A reply left `streaming` by a process that died under the old code path will
-- never be finished by anyone. Its text is what there is.
UPDATE messages
SET status      = 'complete',
    stop_reason = 'cancelled'
WHERE status IN ('pending', 'streaming');

UPDATE messages
SET stop_reason = 'stop'
WHERE role = 'assistant'
  AND stop_reason IS NULL
  AND status = 'complete';

UPDATE messages
SET stop_reason = 'error',
    error_code  = 'model_error'
WHERE role = 'assistant'
  AND status = 'failed';

-- running: someone is working on it, or will be after a restart (a run is
-- resumed from its checkpoint, never abandoned because the process went away).
-- The other three are terminal.
CREATE TYPE run_status AS ENUM ('running', 'completed', 'failed', 'cancelled');

-- One run is the work from a user's input to the final answer: every model
-- request and every tool call in between.
CREATE TABLE runs
(
    id               uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    conversation_id  uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    user_id          uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    input_message_id uuid        NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    -- Chosen by the client and unique per conversation, so a retried POST over a
    -- flaky connection finds the run it already started instead of starting a
    -- second one and paying for the answer twice.
    request_id       text,
    -- What was asked for and where it is answered from. Pinned when the run
    -- starts: an operator disabling the channel afterwards must not move a run
    -- that is halfway through onto a different endpoint.
    model_id         text        NOT NULL,
    channel_id       uuid        REFERENCES channels (id) ON DELETE SET NULL,
    status           run_status  NOT NULL DEFAULT 'running',
    -- The checkpoint a restarted process resumes from, and the presentation of
    -- the work in flight: the partial response and the current tool round.
    -- Small by construction — it holds only what has not become a message yet.
    checkpoint       jsonb       NOT NULL DEFAULT '{"phase": "prepare", "attempt": 1, "round": 0}'::jsonb,
    live             jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Bumped by every commit that touches this run. Events carry it, and a
    -- snapshot carries it, so a subscriber can drop events its snapshot already
    -- contains.
    version          bigint      NOT NULL DEFAULT 1,
    -- Persisted so a stop pressed while the process restarts is still honoured.
    cancel_requested boolean     NOT NULL DEFAULT false,
    error_code       text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    finished_at      timestamptz,
    CONSTRAINT runs_finished_when_terminal
        CHECK ((status = 'running') = (finished_at IS NULL))
);

-- A conversation runs one thing at a time. This is the guarantee; the handler's
-- busy check is only the readable error in front of it.
CREATE UNIQUE INDEX runs_one_active_per_conversation
    ON runs (conversation_id) WHERE status = 'running';

CREATE UNIQUE INDEX runs_request_id
    ON runs (conversation_id, request_id) WHERE request_id IS NOT NULL;

-- What a restarted process scans for.
CREATE INDEX runs_running ON runs (created_at) WHERE status = 'running';

ALTER TABLE messages
    ADD COLUMN run_id uuid REFERENCES runs (id) ON DELETE SET NULL;

CREATE INDEX messages_run_idx ON messages (run_id) WHERE run_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Tool rows have no place in the old one-reply-per-message shape, and an
-- assistant row that only called tools has no text to show.
DELETE FROM messages WHERE role = 'tool';
DELETE FROM messages WHERE role = 'assistant' AND content = '';

ALTER TABLE messages DROP COLUMN run_id;
DROP TABLE runs;
DROP TYPE run_status;

ALTER TABLE messages
    DROP COLUMN parts,
    DROP COLUMN stop_reason,
    DROP COLUMN model,
    DROP COLUMN provider,
    DROP COLUMN usage,
    DROP COLUMN error_code;

-- +goose StatementEnd
