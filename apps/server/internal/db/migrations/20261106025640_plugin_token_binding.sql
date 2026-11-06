-- +goose Up
-- +goose StatementBegin
-- A person's token for a plugin is good for the service it was given by or
-- checked against, and for no other: token_for records which, as
-- '<auth_type> <mcp_url>' at the time. An administrator who points the plugin
-- at another address, or changes how it signs in, leaves the token unused and
-- the person asked to connect again - rather than sent to the new service.
ALTER TABLE plugin_installs ADD COLUMN token_for text;

-- Tokens held now were got from the plugin as it is.
UPDATE plugin_installs i
SET token_for = p.auth_type || ' ' || coalesce(p.mcp_url, '')
FROM plugins p
WHERE p.id = i.plugin_id
  AND i.access_token IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE plugin_installs DROP COLUMN token_for;
-- +goose StatementEnd
