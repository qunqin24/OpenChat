-- +goose Up
-- +goose StatementBegin
-- Each person's own account for a plugin (OAuth 2.1 with PKCE,
-- docs/design/plugins-plan.md §5). Everything secret is sealed with the key in
-- data/secret.key (internal/secrets), never stored in the clear.

-- The OAuth client this site is to the plugin's authorization server: one an
-- administrator registered there (GitHub's, say), or one registered on the
-- fly where the server allows it (registered). Scopes are what is asked for.
ALTER TABLE plugins
    ADD COLUMN oauth_client_id     text    NOT NULL DEFAULT '',
    ADD COLUMN oauth_client_secret bytea,
    ADD COLUMN oauth_scopes        text    NOT NULL DEFAULT '',
    ADD COLUMN oauth_registered    boolean NOT NULL DEFAULT false;

-- A person's tokens, on their install: an install of an OAuth plugin exists
-- only once they connected.
ALTER TABLE plugin_installs
    ADD COLUMN access_token     bytea,
    ADD COLUMN refresh_token    bytea,
    ADD COLUMN token_expires_at timestamptz,
    ADD COLUMN needs_reconnect  boolean NOT NULL DEFAULT false;

-- An authorization on its way: one per sign-in, taken once by the callback,
-- gone after ten minutes. The state is kept as its SHA-256; the PKCE verifier
-- sealed.
CREATE TABLE plugin_oauth_states (
    state_hash   bytea PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    plugin_id    uuid        NOT NULL REFERENCES plugins (id) ON DELETE CASCADE,
    verifier     bytea       NOT NULL,
    redirect_uri text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE plugin_oauth_states;
ALTER TABLE plugin_installs DROP COLUMN needs_reconnect, DROP COLUMN token_expires_at, DROP COLUMN refresh_token,
    DROP COLUMN access_token;
ALTER TABLE plugins DROP COLUMN oauth_registered, DROP COLUMN oauth_scopes, DROP COLUMN oauth_client_secret,
    DROP COLUMN oauth_client_id;
-- +goose StatementEnd
