-- +goose Up
-- +goose StatementBegin

-- Whether a conversation may use the web: searching and reading pages. On by
-- default — the model still decides when a question needs it — and a person
-- switches it off from the composer. A run copies the conversation's choice
-- when it starts, so changing it mid-run applies to the next message.
ALTER TABLE conversations ADD COLUMN web_search boolean NOT NULL DEFAULT true;
ALTER TABLE runs ADD COLUMN web_search boolean NOT NULL DEFAULT true;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE runs DROP COLUMN web_search;
ALTER TABLE conversations DROP COLUMN web_search;
-- +goose StatementEnd
