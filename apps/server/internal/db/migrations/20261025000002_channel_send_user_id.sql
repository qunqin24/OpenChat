-- +goose Up
-- +goose StatementBegin

-- Whether requests through this channel carry the end user's identifier
-- (OpenAI's safety_identifier, Anthropic's metadata.user_id). A third-party
-- endpoint may reject a field it does not know, so it is a switch per channel,
-- on by default only for the vendors' own endpoints: a channel of those types
-- with no base URL of its own, or one whose host is theirs.
ALTER TABLE channels
    ADD COLUMN send_user_id boolean NOT NULL DEFAULT false;

UPDATE channels
SET send_user_id = true
WHERE lower(substring(base_url from '^[A-Za-z][A-Za-z0-9+.-]*://([^/:?#@]*)')) IN ('api.openai.com', 'api.anthropic.com')
   OR (btrim(base_url) = '' AND type IN ('openai', 'anthropic'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE channels
    DROP COLUMN send_user_id;

-- +goose StatementEnd
