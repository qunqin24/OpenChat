-- +goose Up
-- +goose StatementBegin

-- An edited question is a new user message, never a rewrite of the old one:
-- `edit_of` names the message it is a version of — always the first, so every
-- version of a turn points at one root. The edit and its answer join the
-- turn's versions (see 20261011000003): the runs of the root and of each edit
-- are the versions, one shown, and a user message whose every answer is
-- superseded is hidden with them.
ALTER TABLE messages
    ADD COLUMN edit_of uuid REFERENCES messages (id) ON DELETE CASCADE;

CREATE INDEX messages_edit_of_idx ON messages (edit_of) WHERE edit_of IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Without the column the edits would read as questions asked in a row; they
-- go with their answers (a run's rows only lose their run_id when it goes).
DELETE FROM messages
WHERE run_id IN (SELECT r.id
                 FROM runs r
                          JOIN messages m ON m.id = r.input_message_id
                 WHERE m.edit_of IS NOT NULL);
DELETE FROM messages WHERE edit_of IS NOT NULL;

DROP INDEX messages_edit_of_idx;
ALTER TABLE messages DROP COLUMN edit_of;

-- +goose StatementEnd
