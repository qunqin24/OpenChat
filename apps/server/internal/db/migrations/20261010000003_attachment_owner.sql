-- +goose Up
-- +goose StatementBegin

-- Who uploaded a file. Until now an attachment belonged to nobody until a
-- message claimed it, so anyone holding an id could attach another person's
-- upload to their own message and read it back through a signed URL. Claiming
-- one now has to match its uploader. Rows from before this column stay
-- unowned and can no longer be claimed; they were never reachable from the UI.
ALTER TABLE attachments ADD COLUMN user_id uuid REFERENCES users (id) ON DELETE CASCADE;

-- A user's unclaimed uploads, which is what a message's attachment ids are checked against.
CREATE INDEX attachments_user_pending_idx ON attachments (user_id) WHERE message_id IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX attachments_user_pending_idx;
ALTER TABLE attachments DROP COLUMN user_id;
-- +goose StatementEnd
