-- +goose Up
-- +goose StatementBegin

-- `name` is the username now, not a free-text display label, so two accounts
-- can no longer answer to the same one.
--
-- Case-insensitive, for the same reason the email index is: "Alice" and "alice"
-- read as the same person to everyone except a byte comparison, and letting
-- both exist makes "who is alice" ambiguous at exactly the moment it matters.
--
-- Any database with duplicates already fails here. That is deliberate: picking
-- a winner silently would rename somebody's account behind their back. Goose
-- runs each migration in a transaction, so the failure rolls back whole and the
-- duplicates can be resolved by hand first.
CREATE UNIQUE INDEX users_name_lower_idx ON users (lower(name));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS users_name_lower_idx;
-- +goose StatementEnd
