-- +goose Up
-- +goose StatementBegin
-- The person's own answer to "a plugin tool wants to change something":
-- ask each time (the card), or allow every such call without asking.
-- Administrators' blocked tools stay blocked either way.
ALTER TABLE users ADD COLUMN plugin_calls text NOT NULL DEFAULT 'ask' CHECK (plugin_calls IN ('ask', 'allow'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN plugin_calls;
-- +goose StatementEnd
