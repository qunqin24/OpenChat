-- +goose Up
-- +goose StatementBegin
-- Plugins whose service takes each person's own token, issued by the service
-- for their account there (McDonald's China, say) rather than through OAuth:
-- the person pastes it on the plugins page, and it is sealed on their install
-- as an OAuth access token would be (plugin_installs.access_token), with no
-- refresh token and no expiry. token_url is where they get one.
ALTER TABLE plugins DROP CONSTRAINT plugins_auth_type_check;
ALTER TABLE plugins
    ADD CONSTRAINT plugins_auth_type_check CHECK (auth_type IN ('none', 'header', 'oauth', 'token')),
    ADD COLUMN token_url text NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- A token plugin has no way back but to be switched off: its people's tokens
-- go with it, as nothing else could send them.
UPDATE plugin_installs
SET access_token = NULL
WHERE plugin_id IN (SELECT id FROM plugins WHERE auth_type = 'token');
UPDATE plugins
SET auth_type = 'none',
    enabled   = false
WHERE auth_type = 'token';
ALTER TABLE plugins DROP CONSTRAINT plugins_auth_type_check;
ALTER TABLE plugins
    ADD CONSTRAINT plugins_auth_type_check CHECK (auth_type IN ('none', 'header', 'oauth')),
    DROP COLUMN token_url;
-- +goose StatementEnd
