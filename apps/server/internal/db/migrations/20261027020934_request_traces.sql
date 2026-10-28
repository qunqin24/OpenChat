-- +goose Up
-- +goose StatementBegin
-- What a model request carried and what came back, for an administrator
-- debugging their own conversations (the 轨迹 view): the messages, tools and
-- options as sent, and the response. Only an administrator's requests are
-- recorded (api.NewTracer), and only for a week (cleanup.go). The figures -
-- timing, tokens, cost - are the request's usage_logs row.
CREATE TABLE request_traces (
    usage_log_id    uuid PRIMARY KEY REFERENCES usage_logs (id) ON DELETE CASCADE,
    created_at      timestamptz NOT NULL DEFAULT now(),
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    request         jsonb       NOT NULL,
    response        jsonb       NOT NULL
);

CREATE INDEX request_traces_conversation_idx ON request_traces (conversation_id, created_at);
CREATE INDEX request_traces_created_idx ON request_traces (created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE request_traces;
-- +goose StatementEnd
