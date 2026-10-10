-- +goose Up
-- Which of OpenAI's two request formats a channel on that protocol speaks:
-- Chat Completions (`chat`, what nearly every compatible endpoint serves) or
-- the newer Responses API (`responses`). Other protocols ignore it.
ALTER TABLE channels
    ADD COLUMN api_format text NOT NULL DEFAULT 'chat'
        CONSTRAINT channels_api_format CHECK (api_format IN ('chat', 'responses'));

-- +goose Down
ALTER TABLE channels DROP COLUMN api_format;
