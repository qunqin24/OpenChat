-- +goose Up
-- +goose StatementBegin

-- An input can be answered more than once: "regenerate" starts another run on
-- the same input message, and the earlier answer is kept as a version the
-- reader can switch back to. Exactly one run per input is shown, and sent to
-- the model as history; the others are superseded. The flag is on the run, not
-- its rows, so transcript rows stay as they were written.
ALTER TABLE runs
    ADD COLUMN superseded boolean NOT NULL DEFAULT false;

-- The versions of an input, and what regenerating supersedes.
CREATE INDEX runs_input_message_idx ON runs (input_message_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Without the flag every version would show at once; the hidden ones go.
DELETE FROM messages WHERE run_id IN (SELECT id FROM runs WHERE superseded);
DELETE FROM runs WHERE superseded;

DROP INDEX runs_input_message_idx;
ALTER TABLE runs DROP COLUMN superseded;

-- +goose StatementEnd
