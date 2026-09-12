-- +goose Up
-- +goose StatementBegin
CREATE TYPE user_role AS ENUM ('user', 'admin');

ALTER TABLE users
    ADD COLUMN password_hash TEXT      NOT NULL DEFAULT '',
    ADD COLUMN role          user_role NOT NULL DEFAULT 'user',
    -- Disabling is reversible and keeps the row, so the person's conversations
    -- and usage history survive; deleting the user would cascade them away.
    ADD COLUMN disabled_at   TIMESTAMPTZ,
    ADD COLUMN last_seen_at  TIMESTAMPTZ;

-- Addresses are compared case-insensitively, so uniqueness has to be too:
-- the plain UNIQUE on `email` would happily accept Alice@x.com next to
-- alice@x.com and leave the login lookup ambiguous.
-- Dropping the constraint takes its backing index with it; dropping the index
-- first is refused precisely because the constraint still needs it.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

-- The cookie carries a random token; only its SHA-256 is stored here, so a
-- leaked database cannot be replayed as a login.
CREATE TABLE sessions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   BYTEA       NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_agent   TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- Conversations created before this migration have no owner. They are left in
-- place rather than deleted, but the owner filter means nobody will list them.
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS sessions;

DROP INDEX IF EXISTS users_email_lower_idx;
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);

ALTER TABLE users
    DROP COLUMN IF EXISTS last_seen_at,
    DROP COLUMN IF EXISTS disabled_at,
    DROP COLUMN IF EXISTS role,
    DROP COLUMN IF EXISTS password_hash;

DROP TYPE IF EXISTS user_role;
-- +goose StatementEnd
