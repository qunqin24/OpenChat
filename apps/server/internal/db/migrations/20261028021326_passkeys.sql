-- +goose Up
-- +goose StatementBegin
-- Passkeys (api/passkeys.go): WebAuthn credentials an account signs in with
-- instead of a password. The credential as go-webauthn keeps it - public key,
-- sign count, flags, transports - is one jsonb; its id is apart, for the
-- lookup a sign-in makes by it. They go with their account.
CREATE TABLE passkeys
(
    id            uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    credential_id bytea       NOT NULL UNIQUE,
    credential    jsonb       NOT NULL,
    name          text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz
);
CREATE INDEX passkeys_user_idx ON passkeys (user_id);

-- A ceremony begun and not yet finished: the challenge the browser must sign,
-- kept until the answer comes or five minutes pass. A registration names its
-- account; a sign-in does not, since the passkey says whose it is.
CREATE TABLE webauthn_challenges
(
    id         uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    kind       text        NOT NULL CHECK (kind IN ('register', 'login')),
    user_id    uuid REFERENCES users (id) ON DELETE CASCADE,
    session    jsonb       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE webauthn_challenges;
DROP TABLE passkeys;
-- +goose StatementEnd
