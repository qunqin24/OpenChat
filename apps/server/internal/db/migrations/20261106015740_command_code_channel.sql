-- +goose Up

-- Command Code's Provider API, a gateway that serves each model on the API its
-- models list names. The routing lives in the served table in
-- `internal/api/channel_types.go`; the database only has to accept the name.
--
-- ADD VALUE is allowed inside goose's transaction because nothing in this
-- migration uses the new value.
ALTER TYPE channel_type ADD VALUE 'command-code' BEFORE 'openai-compatible';

-- +goose Down
-- +goose StatementBegin

-- Postgres cannot drop an enum value, so the type is rebuilt. A Command Code
-- channel becomes the generic relay at the same address: its GPT and open
-- models keep working over Chat Completions, while Claude — which it serves on
-- Messages only — stops until the channel is reconfigured.
UPDATE channels
SET base_url = 'https://api.commandcode.ai/provider/v1'
WHERE type::text = 'command-code'
  AND base_url = '';

ALTER TABLE channels ALTER COLUMN type DROP DEFAULT;
ALTER TYPE channel_type RENAME TO channel_type_new;
CREATE TYPE channel_type AS ENUM (
    'openai', 'anthropic', 'gemini',
    'openrouter', 'deepseek', 'siliconflow', 'moonshot', 'zhipu',
    'dashscope', 'volcengine', 'minimax', 'xai', 'groq', 'ollama',
    'mistral', 'stepfun', 'hunyuan', 'qianfan', 'mimo', 'together',
    'fireworks', 'nvidia', 'modelscope', 'cerebras', 'lmstudio',
    'opencode-go', 'openai-compatible'
    );
ALTER TABLE channels
    ALTER COLUMN type TYPE channel_type USING (
        CASE WHEN type::text = 'command-code' THEN 'openai-compatible' ELSE type::text END
        )::channel_type;
ALTER TABLE channels ALTER COLUMN type SET DEFAULT 'openai';
DROP TYPE channel_type_new;

-- +goose StatementEnd
