-- +goose Up
-- +goose StatementBegin
-- The administrator who disabled an account: the one the person is told to
-- contact. NULL for a moderation ban, for accounts disabled before this was
-- recorded, and once that administrator's account is deleted.
ALTER TABLE users ADD COLUMN disabled_by uuid REFERENCES users (id) ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN IF EXISTS disabled_by;
-- +goose StatementEnd
