-- +goose Up
-- +goose StatementBegin

-- Reasoning effort now takes every level a provider uses, not three: which
-- ones a model accepts comes from its models.dev `reasoning_options`, and a
-- budget-controlled model maps low/medium/high onto budgets.
ALTER TABLE conversations DROP CONSTRAINT conversations_reasoning_effort;
ALTER TABLE conversations
    ADD CONSTRAINT conversations_reasoning_effort
        CHECK (reasoning_effort IN ('none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'));

ALTER TABLE runs DROP CONSTRAINT runs_reasoning_effort;
ALTER TABLE runs
    ADD CONSTRAINT runs_reasoning_effort
        CHECK (reasoning_effort IN ('none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE conversations SET reasoning_effort = NULL WHERE reasoning_effort NOT IN ('low', 'medium', 'high');
UPDATE runs SET reasoning_effort = NULL WHERE reasoning_effort NOT IN ('low', 'medium', 'high');
ALTER TABLE conversations DROP CONSTRAINT conversations_reasoning_effort;
ALTER TABLE conversations
    ADD CONSTRAINT conversations_reasoning_effort CHECK (reasoning_effort IN ('low', 'medium', 'high'));
ALTER TABLE runs DROP CONSTRAINT runs_reasoning_effort;
ALTER TABLE runs
    ADD CONSTRAINT runs_reasoning_effort CHECK (reasoning_effort IN ('low', 'medium', 'high'));
-- +goose StatementEnd
