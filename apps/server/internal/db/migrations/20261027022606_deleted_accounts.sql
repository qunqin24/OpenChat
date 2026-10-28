-- +goose Up
-- +goose StatementBegin
-- An account an administrator deleted (DeleteUser) leaves its requests in the
-- ledger - spend and caps are counted from it - with the owner set to NULL by
-- the foreign key. Until now an ownerless request was always a guest's; this
-- is what tells the two apart: the name the account had.
ALTER TABLE usage_logs ADD COLUMN deleted_user text;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE usage_logs DROP COLUMN deleted_user;
-- +goose StatementEnd
