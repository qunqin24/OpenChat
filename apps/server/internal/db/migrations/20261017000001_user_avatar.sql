-- +goose Up
-- +goose StatementBegin

-- The storage key of the account's picture, NULL for the initial on a colour.
-- A key rather than a URL: the object is read through whichever signed URL the
-- storage driver issues at the time. Each upload gets a key of its own, so a
-- replaced picture is a new address and no cache holds on to the old one.
ALTER TABLE users
    ADD COLUMN avatar_key TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users
    DROP COLUMN avatar_key;
-- +goose StatementEnd
