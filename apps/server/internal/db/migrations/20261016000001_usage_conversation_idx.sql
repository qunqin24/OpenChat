-- +goose Up
-- +goose StatementBegin

-- The composer's context meter reads a conversation's latest request: how much
-- of the model's window it filled (LatestContextTokens).
CREATE INDEX usage_logs_conversation_idx ON usage_logs (conversation_id, created_at DESC)
    WHERE conversation_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX usage_logs_conversation_idx;
-- +goose StatementEnd
