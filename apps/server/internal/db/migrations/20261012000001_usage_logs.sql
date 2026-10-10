-- +goose Up
-- +goose StatementBegin

-- One row per request made to a model: each attempt of a run (a retry is its
-- own row, and so is a failure), and each background title. The ledger an
-- operator reads to see who used what, what it cost and how fast it came back.
--
-- Written once and never changed. Everything that can later change underneath
-- it is copied in rather than joined: the channel's name (channels get renamed
-- and deleted), the prices (the catalogue is refreshed daily) and the cost that
-- followed from them. A row says what happened then, not what would happen now.
--
-- The links are SET NULL rather than CASCADE: this is accounting, and deleting
-- a conversation or a user must not make the money spent on it disappear from
-- the totals. (Accounts are only ever disabled today; the rule is for the day
-- that stops being true.)
CREATE TABLE usage_logs
(
    id               uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    -- When the request started, not when its row was written.
    created_at       timestamptz NOT NULL DEFAULT now(),
    -- `chat` is a model request of a run; `title` is the background request
    -- that names a conversation.
    kind             text        NOT NULL
        CONSTRAINT usage_logs_kind CHECK (kind IN ('chat', 'title')),
    -- A user pressing Stop is `ok`: what was generated is kept, as in the
    -- transcript. Only a failed request is `error`.
    status           text        NOT NULL
        CONSTRAINT usage_logs_status CHECK (status IN ('ok', 'error')),
    -- The machine code the chat shows (model_auth, model_rate_limited, ...) and
    -- a short, scrubbed, truncated message for the operator.
    error_code       text,
    error_message    text,

    user_id          uuid        REFERENCES users (id) ON DELETE SET NULL,
    conversation_id  uuid        REFERENCES conversations (id) ON DELETE SET NULL,
    run_id           uuid        REFERENCES runs (id) ON DELETE SET NULL,
    channel_id       uuid        REFERENCES channels (id) ON DELETE SET NULL,
    channel_name     text        NOT NULL DEFAULT '',

    -- What was asked for, and what the provider says it served.
    model_id         text        NOT NULL,
    served_model     text        NOT NULL DEFAULT '',

    -- llm.Usage, field for field: input excludes the cached part, output
    -- includes reasoning.
    input_tokens       integer   NOT NULL DEFAULT 0,
    output_tokens      integer   NOT NULL DEFAULT 0,
    reasoning_tokens   integer   NOT NULL DEFAULT 0,
    cache_read_tokens  integer   NOT NULL DEFAULT 0,
    cache_write_tokens integer   NOT NULL DEFAULT 0,

    -- USD per million tokens as the catalogue priced the model at the time;
    -- NULL where it did not say.
    input_price        numeric,
    output_price       numeric,
    cache_read_price   numeric,
    cache_write_price  numeric,
    -- NULL when the price is unknown or the provider reported no usage: an
    -- unpriced request is not a free one.
    cost               numeric,

    streamed         boolean     NOT NULL DEFAULT true,
    -- Request start to the first content, reasoning or tool-call delta.
    first_token_ms   integer,
    duration_ms      integer     NOT NULL DEFAULT 0
);

-- The list is newest first and the filters are user, model and channel, each
-- usually combined with that order.
CREATE INDEX usage_logs_created_idx ON usage_logs (created_at DESC, id DESC);
CREATE INDEX usage_logs_user_idx ON usage_logs (user_id, created_at DESC);
CREATE INDEX usage_logs_model_idx ON usage_logs (model_id, created_at DESC);
CREATE INDEX usage_logs_channel_idx ON usage_logs (channel_id, created_at DESC);
CREATE INDEX usage_logs_errors_idx ON usage_logs (created_at DESC) WHERE status = 'error';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE usage_logs;
-- +goose StatementEnd
