-- +goose Up
-- +goose StatementBegin

-- Referring to earlier conversations (docs/design/chat-history-plan.md): a new
-- conversation is given a list of what the person said in their recent ones,
-- and the model may search and read the rest. The person's own switch, on
-- unless they turn it off; the administrator's is the setting `chat_history`.
ALTER TABLE users
    ADD COLUMN chat_history boolean NOT NULL DEFAULT true;

-- A title goes into those lists, and the model reads it there. One a person
-- typed was never moderated, so it is checked when a list would carry it and
-- the verdict is kept here by the hash of the title it was about: renaming
-- makes it unchecked again. One the server generated is recorded as checked
-- when it is written.
ALTER TABLE conversations
    ADD COLUMN title_checked_hash text,
    ADD COLUMN title_blocked_hash text;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE conversations
    DROP COLUMN title_blocked_hash,
    DROP COLUMN title_checked_hash;

ALTER TABLE users
    DROP COLUMN chat_history;

-- +goose StatementEnd
