-- +goose Up
-- +goose StatementBegin

-- How hard a reasoning model thinks. NULL is "automatic": nothing is sent and
-- the model does what it does by default — for Claude, no extended thinking;
-- for o-series and Gemini models, their own default effort.
--
-- Remembered on the conversation, like its model, and pinned on each run when
-- it starts, so changing it mid-answer affects the next answer.
ALTER TABLE conversations
    ADD COLUMN reasoning_effort text
        CONSTRAINT conversations_reasoning_effort CHECK (reasoning_effort IN ('low', 'medium', 'high'));

ALTER TABLE runs
    ADD COLUMN reasoning_effort text
        CONSTRAINT runs_reasoning_effort CHECK (reasoning_effort IN ('low', 'medium', 'high'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN reasoning_effort;
ALTER TABLE conversations DROP COLUMN reasoning_effort;
-- +goose StatementEnd
