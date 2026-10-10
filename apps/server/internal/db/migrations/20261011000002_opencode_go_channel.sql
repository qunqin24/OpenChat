-- +goose Up

-- OpenCode Go, a gateway that serves each model on its maker's API. The routing
-- lives in the served table in `internal/api/channel_types.go`; the database
-- only has to accept the name.
--
-- ADD VALUE is allowed inside goose's transaction because nothing in this
-- migration uses the new value.
ALTER TYPE channel_type ADD VALUE 'opencode-go' BEFORE 'openai-compatible';

-- +goose Down
-- +goose StatementBegin

-- Postgres cannot drop an enum value, so the type is rebuilt. An OpenCode Go
-- channel becomes the generic relay at the same address: its open models keep
-- working over Chat Completions, while Claude, GPT and Grok — which it served on
-- other APIs — stop until the channel is reconfigured.
UPDATE channels
SET base_url = 'https://opencode.ai/zen/go/v1'
WHERE type::text = 'opencode-go'
  AND base_url = '';

ALTER TABLE channels ALTER COLUMN type DROP DEFAULT;
ALTER TYPE channel_type RENAME TO channel_type_new;
CREATE TYPE channel_type AS ENUM (
    'openai', 'anthropic', 'gemini',
    'openrouter', 'deepseek', 'siliconflow', 'moonshot', 'zhipu',
    'dashscope', 'volcengine', 'minimax', 'xai', 'groq', 'ollama',
    'mistral', 'stepfun', 'hunyuan', 'qianfan', 'mimo', 'together',
    'fireworks', 'nvidia', 'modelscope', 'cerebras', 'lmstudio',
    'openai-compatible'
    );
ALTER TABLE channels
    ALTER COLUMN type TYPE channel_type USING (
        CASE WHEN type::text = 'opencode-go' THEN 'openai-compatible' ELSE type::text END
        )::channel_type;
ALTER TABLE channels ALTER COLUMN type SET DEFAULT 'openai';
DROP TYPE channel_type_new;

-- +goose StatementEnd
