-- +goose Up
-- +goose StatementBegin

-- Guest mode keeps nothing of a guest on the server. A guest's request is
-- answered by the same machinery as anyone's, which wants a user, a
-- conversation and a transcript in the database; those rows are written for the
-- length of one run and deleted when it ends. The user is flagged `guest` so
-- that the places that must never see it - sign-in, the admin's user list and
-- counts, the "first account is the administrator" rule - can leave it out.
-- A guest has no password and no session, ever.
ALTER TABLE users
    ADD COLUMN guest boolean NOT NULL DEFAULT false;

-- What the sweep for runs a crashed process left behind reads.
CREATE INDEX users_guest_idx ON users (created_at) WHERE guest;

-- One row per guest request admitted, for the limits an administrator sets:
-- requests per address per hour and in all per day. A table rather than memory,
-- so several servers count together; rows older than a day are purged by the
-- cleanup loop. The address is the client's, as the router's RealIP middleware
-- reports it.
CREATE TABLE guest_requests
(
    ip         text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX guest_requests_ip_idx ON guest_requests (ip, created_at DESC);
CREATE INDEX guest_requests_created_idx ON guest_requests (created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE guest_requests;
DROP INDEX users_guest_idx;
-- A guest left behind by a rollback would be an account nobody can sign in to
-- and nobody lists; its rows go with it.
DELETE FROM users WHERE guest;
ALTER TABLE users
    DROP COLUMN guest;

-- +goose StatementEnd
