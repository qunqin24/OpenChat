-- +goose Up
-- +goose StatementBegin

-- `kind` held the wire protocol and `provider_id` held the brand, and an
-- operator adding a channel had to answer both — one of which ("which protocol
-- does this speak") they should never have been asked. A single `type` answers
-- both: it names the vendor, and the server looks up the protocol from it.
--
-- The old three-value enum is a subset of the new one by name, so the existing
-- rows carry over unchanged.
CREATE TYPE channel_type AS ENUM (
    'openai', 'anthropic', 'gemini',
    'openrouter', 'deepseek', 'siliconflow', 'moonshot', 'zhipu',
    'dashscope', 'volcengine', 'minimax', 'xai', 'groq', 'ollama',
    'openai-compatible'
    );

ALTER TABLE channels
    ADD COLUMN type channel_type NOT NULL DEFAULT 'openai';

UPDATE channels SET type = kind::text::channel_type;

ALTER TABLE channels
    DROP COLUMN kind,
    DROP COLUMN provider_id;

DROP TYPE channel_kind;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TYPE channel_kind AS ENUM ('openai', 'anthropic', 'gemini');
ALTER TABLE channels
    ADD COLUMN kind channel_kind NOT NULL DEFAULT 'openai',
    ADD COLUMN provider_id text;
UPDATE channels
SET kind = CASE type::text
               WHEN 'anthropic' THEN 'anthropic'::channel_kind
               WHEN 'gemini' THEN 'gemini'::channel_kind
               ELSE 'openai'::channel_kind END;
ALTER TABLE channels DROP COLUMN type;
DROP TYPE channel_type;
-- +goose StatementEnd
