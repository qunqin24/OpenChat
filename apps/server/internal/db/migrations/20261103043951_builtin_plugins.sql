-- +goose Up
-- +goose StatementBegin
-- Plugins that ship with OpenChat (internal/plugins/builtin.go): the key of
-- the definition a row was made from. Made once, at a start that finds it
-- missing; an administrator may change or turn one off, not delete it.
ALTER TABLE plugins ADD COLUMN builtin text UNIQUE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE plugins DROP COLUMN builtin;
-- +goose StatementEnd
