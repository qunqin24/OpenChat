-- +goose Up
-- +goose StatementBegin

-- Sign-in takes a username or an email in the same field, so the two name
-- spaces have to stay disjoint. Excluding `@` from usernames is what makes
-- that true: without it, an account could take a username spelled exactly like
-- someone else's address, and the login lookup would have two rows to choose
-- between for one input.
--
-- The constraint rather than validation alone, because validation is a
-- property of the four handlers that happen to write this column today, and
-- this is a property of the column.
--
-- A database with offending rows fails here, on purpose: renaming somebody's
-- account to satisfy a constraint is not a migration's decision to make. Goose
-- runs each migration in a transaction, so the failure rolls back whole.
ALTER TABLE users
    ADD CONSTRAINT users_name_no_at CHECK (position('@' in name) = 0);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_name_no_at;
-- +goose StatementEnd
