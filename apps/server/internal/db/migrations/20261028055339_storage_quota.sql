-- +goose Up
-- +goose StatementBegin
-- How much a person may keep in files: NULL is the instance's default (the
-- storage_quota setting), a number an administrator's choice for this one.
-- What they keep is summed over their attachments, by the library's index on
-- (user_id, created_at, id).
ALTER TABLE users ADD COLUMN storage_quota_bytes BIGINT CHECK (storage_quota_bytes >= 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN IF EXISTS storage_quota_bytes;
-- +goose StatementEnd
