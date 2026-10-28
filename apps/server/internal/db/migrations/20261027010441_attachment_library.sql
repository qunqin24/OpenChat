-- +goose Up
-- +goose StatementBegin
-- The library: a user's sent files, newest first (ListLibraryFiles). The
-- pending index covers only the unclaimed ones.
CREATE INDEX attachments_user_library_idx
    ON attachments (user_id, created_at DESC, id DESC)
    WHERE message_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX attachments_user_library_idx;
-- +goose StatementEnd
