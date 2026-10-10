-- +goose Up

-- Ten more vendors for the channel picker. Every one of them speaks the OpenAI
-- protocol, so nothing but the enum changes here: the protocol, the endpoint and
-- the key format live in the served table in `internal/api/channel_types.go`.
--
-- ADD VALUE is allowed inside goose's transaction because nothing in this
-- migration uses the new values.
ALTER TYPE channel_type ADD VALUE 'mistral' BEFORE 'openai-compatible';
ALTER TYPE channel_type ADD VALUE 'stepfun' BEFORE 'openai-compatible';
ALTER TYPE channel_type ADD VALUE 'hunyuan' BEFORE 'openai-compatible';
ALTER TYPE channel_type ADD VALUE 'qianfan' BEFORE 'openai-compatible';
ALTER TYPE channel_type ADD VALUE 'mimo' BEFORE 'openai-compatible';
ALTER TYPE channel_type ADD VALUE 'together' BEFORE 'openai-compatible';
ALTER TYPE channel_type ADD VALUE 'fireworks' BEFORE 'openai-compatible';
ALTER TYPE channel_type ADD VALUE 'nvidia' BEFORE 'openai-compatible';
ALTER TYPE channel_type ADD VALUE 'modelscope' BEFORE 'openai-compatible';
ALTER TYPE channel_type ADD VALUE 'cerebras' BEFORE 'openai-compatible';
ALTER TYPE channel_type ADD VALUE 'lmstudio' BEFORE 'openai-compatible';

-- +goose Down
-- +goose StatementBegin

-- Postgres cannot drop an enum value, so the type is rebuilt. A channel using
-- one of the new vendors keeps its endpoint and key and becomes the generic
-- relay, which speaks the same protocol — it degrades rather than disappears.
-- Its stored base URL may be blank (meaning "the vendor's default"), which the
-- relay has no default for, so the old default is written in.
UPDATE channels
SET base_url = CASE type::text
                   WHEN 'mistral' THEN 'https://api.mistral.ai/v1'
                   WHEN 'stepfun' THEN 'https://api.stepfun.com/v1'
                   WHEN 'hunyuan' THEN 'https://api.hunyuan.cloud.tencent.com/v1'
                   WHEN 'qianfan' THEN 'https://qianfan.baidubce.com/v2'
                   WHEN 'mimo' THEN 'https://api.xiaomimimo.com/v1'
                   WHEN 'together' THEN 'https://api.together.xyz/v1'
                   WHEN 'fireworks' THEN 'https://api.fireworks.ai/inference/v1'
                   WHEN 'nvidia' THEN 'https://integrate.api.nvidia.com/v1'
                   WHEN 'modelscope' THEN 'https://api-inference.modelscope.cn/v1'
                   WHEN 'cerebras' THEN 'https://api.cerebras.ai/v1'
                   WHEN 'lmstudio' THEN 'http://localhost:1234/v1'
    END
WHERE type::text IN ('mistral', 'stepfun', 'hunyuan', 'qianfan', 'mimo', 'together',
                     'fireworks', 'nvidia', 'modelscope', 'cerebras', 'lmstudio')
  AND base_url = '';

ALTER TABLE channels ALTER COLUMN type DROP DEFAULT;
ALTER TYPE channel_type RENAME TO channel_type_new;
CREATE TYPE channel_type AS ENUM (
    'openai', 'anthropic', 'gemini',
    'openrouter', 'deepseek', 'siliconflow', 'moonshot', 'zhipu',
    'dashscope', 'volcengine', 'minimax', 'xai', 'groq', 'ollama',
    'openai-compatible'
    );
ALTER TABLE channels
    ALTER COLUMN type TYPE channel_type USING (
        CASE
            WHEN type::text IN ('mistral', 'stepfun', 'hunyuan', 'qianfan', 'mimo', 'together',
                                'fireworks', 'nvidia', 'modelscope', 'cerebras', 'lmstudio')
                THEN 'openai-compatible'
            ELSE type::text
            END
        )::channel_type;
ALTER TABLE channels ALTER COLUMN type SET DEFAULT 'openai';
DROP TYPE channel_type_new;

-- +goose StatementEnd
