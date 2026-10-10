-- +goose Up
-- +goose StatementBegin

-- An account waiting for an administrator to let it in. Registration creates
-- one while "new users need approval" is on; a pending account cannot sign in
-- and has no session. The column defaults to false, so every account that
-- exists when this runs - and every account created by an administrator, or by
-- a test inserting rows by hand - counts as approved. Only registration sets it.
ALTER TABLE users
    ADD COLUMN pending boolean NOT NULL DEFAULT false;

-- What the admin list's "pending" filter reads.
CREATE INDEX users_pending_idx ON users (created_at) WHERE pending;

-- One row per sign-in or sign-up attempt that counts against a limit
-- (api/auth_limits.go). A table rather than memory, so several servers count
-- together; the cleanup loop purges rows older than the longest window.
--   kind    'login' (a failed attempt, or one still being checked) or 'register'
--   ip      the client's address, as the guest limits see it (IPv6 as its /64)
--   subject for a login, a hash of the identifier typed; '' for a sign-up. The
--           hash keeps what people typed - an address, perhaps a password in
--           the wrong box - out of the table.
CREATE TABLE auth_attempts
(
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind       text        NOT NULL,
    ip         text        NOT NULL,
    subject    text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX auth_attempts_ip_idx ON auth_attempts (kind, ip, created_at DESC);
CREATE INDEX auth_attempts_created_idx ON auth_attempts (created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE auth_attempts;
DROP INDEX users_pending_idx;
ALTER TABLE users
    DROP COLUMN pending;

-- +goose StatementEnd
