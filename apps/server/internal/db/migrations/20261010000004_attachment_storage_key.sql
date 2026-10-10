-- +goose Up
-- +goose StatementBegin

-- The local storage driver's URLs name an object by its key, and the handler
-- serving one finds the upload it belongs to by that key. Keys carry the
-- upload's own uuid, so they were unique already; now it is stated.
CREATE UNIQUE INDEX attachments_storage_key_idx ON attachments (storage_key);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX attachments_storage_key_idx;
-- +goose StatementEnd
