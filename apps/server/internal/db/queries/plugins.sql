-- Plugins (api/plugins.go, docs/design/plugins-plan.md).

-- name: ListUserPlugins :many
-- The plugins a person may install - on, and for everyone or for
-- administrators when they are one - with whether they installed each and
-- what tools it offers that are not blocked.
SELECT p.*,
       (i.user_id IS NOT NULL)::boolean AS installed,
       coalesce(i.needs_reconnect, false)::boolean AS needs_reconnect,
       (SELECT count(*) FROM plugin_tools t WHERE t.plugin_id = p.id AND t.permission <> 'blocked' AND t.read_only)::bigint     AS read_tools,
       (SELECT count(*) FROM plugin_tools t WHERE t.plugin_id = p.id AND t.permission <> 'blocked' AND NOT t.read_only)::bigint AS write_tools
FROM plugins p
         LEFT JOIN plugin_installs i ON i.plugin_id = p.id AND i.user_id = sqlc.arg('user_id')
WHERE p.enabled
  AND (p.audience = 'all' OR sqlc.arg('admin')::boolean)
ORDER BY p.featured DESC, p.sort, p.name;

-- name: GetUserPlugin :one
-- One plugin as ListUserPlugins has it; no row when the person may not see it.
SELECT p.*,
       (i.user_id IS NOT NULL)::boolean AS installed,
       coalesce(i.needs_reconnect, false)::boolean AS needs_reconnect,
       (SELECT count(*) FROM plugin_tools t WHERE t.plugin_id = p.id AND t.permission <> 'blocked' AND t.read_only)::bigint     AS read_tools,
       (SELECT count(*) FROM plugin_tools t WHERE t.plugin_id = p.id AND t.permission <> 'blocked' AND NOT t.read_only)::bigint AS write_tools
FROM plugins p
         LEFT JOIN plugin_installs i ON i.plugin_id = p.id AND i.user_id = sqlc.arg('user_id')
WHERE p.id = sqlc.arg('id')
  AND p.enabled
  AND (p.audience = 'all' OR sqlc.arg('admin')::boolean);

-- name: InstallPlugin :exec
INSERT INTO plugin_installs (user_id, plugin_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: UninstallPlugin :execrows
DELETE
FROM plugin_installs
WHERE user_id = $1
  AND plugin_id = $2;

-- name: ListAdminPlugins :many
-- Every plugin, for the administrators' page, with how many installed it and
-- how many of its tools are not blocked.
SELECT p.*,
       (SELECT count(*) FROM plugin_installs i WHERE i.plugin_id = p.id)::bigint                       AS installs,
       (SELECT count(*) FROM plugin_tools t WHERE t.plugin_id = p.id)::bigint                          AS tools,
       (SELECT count(*) FROM plugin_tools t WHERE t.plugin_id = p.id AND t.permission <> 'blocked')::bigint AS tools_on
FROM plugins p
ORDER BY p.featured DESC, p.sort, p.name;

-- name: GetPlugin :one
SELECT *
FROM plugins
WHERE id = $1;

-- name: CreatePlugin :one
INSERT INTO plugins (slug, name, description, about, category, icon_text, icon_color, featured, enabled, audience,
                     mcp_url, auth_type, header_name, header_secret, sort, icon_slug, icon_path, direct_tools)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
RETURNING *;

-- name: UpdatePlugin :one
-- The whole plugin as the form has it; the header's value only when given.
UPDATE plugins
SET slug          = $2,
    name          = $3,
    description   = $4,
    about         = $5,
    category      = $6,
    icon_text     = $7,
    icon_color    = $8,
    featured      = $9,
    enabled       = $10,
    audience      = $11,
    mcp_url       = $12,
    auth_type     = $13,
    header_name   = $14,
    header_secret = CASE WHEN sqlc.arg('set_secret')::boolean THEN sqlc.narg('header_secret')::bytea ELSE header_secret END,
    sort          = $15,
    icon_slug     = $16,
    icon_path     = $17,
    direct_tools  = $18,
    updated_at    = now()
WHERE id = $1
RETURNING *;

-- name: DeletePlugin :execrows
-- A plugin an administrator added; a built-in one is turned off instead.
DELETE
FROM plugins
WHERE id = $1
  AND builtin IS NULL;

-- name: ListPluginTools :many
SELECT *
FROM plugin_tools
WHERE plugin_id = $1
ORDER BY name;

-- name: UpsertPluginTool :exec
-- A tool as the service now lists it. A new one starts allowed when it only
-- reads and asking otherwise; one already known keeps the permission the
-- administrator gave it.
INSERT INTO plugin_tools (plugin_id, name, title, description, input_schema, read_only, destructive, permission)
VALUES ($1, $2, $3, $4, $5, $6, $7, CASE WHEN $6::boolean THEN 'allow' ELSE 'confirm' END)
ON CONFLICT (plugin_id, name) DO UPDATE
    SET title        = excluded.title,
        description  = excluded.description,
        input_schema = excluded.input_schema,
        read_only    = excluded.read_only,
        destructive  = excluded.destructive;

-- name: DeletePluginToolsExcept :exec
-- Tools the service no longer lists.
DELETE
FROM plugin_tools
WHERE plugin_id = $1
  AND NOT (name = ANY (sqlc.arg('names')::text[]));

-- name: MarkPluginToolsFetched :exec
UPDATE plugins
SET tools_fetched_at = now()
WHERE id = $1;

-- name: SetPluginToolPermission :execrows
UPDATE plugin_tools
SET permission = $3
WHERE plugin_id = $1
  AND name = $2;

-- name: InsertBuiltinPlugin :execrows
-- A built-in plugin, unless it was made before (or its short name is taken).
-- Starts switched off: it needs an administrator's setup before anyone can use it.
INSERT INTO plugins (builtin, slug, name, description, about, category, icon_text, icon_color, mcp_url, auth_type,
                     enabled, sort, icon_slug, icon_path, oauth_scopes)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, false, $11, $12, $13, $14)
ON CONFLICT DO NOTHING;

-- name: FillBuiltinPluginIcon :exec
-- A built-in plugin's icon, for a row made before the definition had one; an
-- icon an administrator picked stays.
UPDATE plugins
SET icon_slug  = sqlc.arg('icon_slug'),
    icon_path  = sqlc.arg('icon_path'),
    icon_color = CASE WHEN icon_color = '' OR icon_slug = '' THEN sqlc.arg('icon_color') ELSE icon_color END
WHERE builtin = sqlc.arg('builtin')
  AND icon_path = '';

-- name: SetPluginOAuthClient :exec
-- The OAuth client as an administrator gives it; the secret only when given.
UPDATE plugins
SET oauth_client_id     = $2,
    oauth_client_secret = CASE WHEN sqlc.arg('set_secret')::boolean THEN sqlc.narg('secret')::bytea ELSE oauth_client_secret END,
    oauth_scopes        = $3,
    oauth_registered    = false,
    updated_at          = now()
WHERE id = $1;

-- name: SetPluginRegisteredClient :exec
-- A client registered on the fly (RFC 7591), unless an administrator gave one meanwhile.
UPDATE plugins
SET oauth_client_id     = $2,
    oauth_client_secret = $3,
    oauth_registered    = true
WHERE id = $1
  AND oauth_client_id = '';

-- name: CreatePluginOAuthState :exec
INSERT INTO plugin_oauth_states (state_hash, user_id, plugin_id, verifier, redirect_uri)
VALUES ($1, $2, $3, $4, $5);

-- name: TakePluginOAuthState :one
-- An authorization's state, taken once: it is gone whatever happens next.
DELETE
FROM plugin_oauth_states
WHERE state_hash = $1
  AND user_id = $2
  AND created_at > now() - interval '10 minutes'
RETURNING *;

-- name: DeleteStalePluginOAuthStates :execrows
DELETE
FROM plugin_oauth_states
WHERE created_at <= now() - interval '10 minutes';

-- name: SavePluginTokens :exec
-- A person's tokens after connecting or refreshing: the install is made, or brought up to date.
INSERT INTO plugin_installs (user_id, plugin_id, access_token, refresh_token, token_expires_at, needs_reconnect)
VALUES ($1, $2, $3, $4, $5, false)
ON CONFLICT (user_id, plugin_id) DO UPDATE
    SET access_token     = excluded.access_token,
        refresh_token    = coalesce(excluded.refresh_token, plugin_installs.refresh_token),
        token_expires_at = excluded.token_expires_at,
        needs_reconnect  = false;

-- name: GetPluginInstall :one
SELECT *
FROM plugin_installs
WHERE user_id = $1
  AND plugin_id = $2;

-- name: MarkPluginNeedsReconnect :exec
UPDATE plugin_installs
SET needs_reconnect = true
WHERE user_id = $1
  AND plugin_id = $2;

-- name: FillBuiltinPluginScopes :exec
-- A built-in plugin's scopes, for a row made before the definition had them.
UPDATE plugins
SET oauth_scopes = $2
WHERE builtin = $1
  AND oauth_scopes = '';

-- Calling plugin tools from conversations (api/plugin_code.go).

-- name: ListUsablePluginTools :many
-- The tools a person's runs may call: of the plugins they installed that are
-- on and theirs to see, connected when they act as the person, not blocked.
SELECT p.id          AS plugin_id,
       p.slug,
       p.name        AS plugin_name,
       p.description AS plugin_description,
       p.direct_tools,
       p.icon_text,
       p.icon_color,
       p.icon_path,
       t.name,
       t.title,
       t.description,
       t.input_schema,
       t.read_only,
       t.permission
FROM plugin_installs i
         JOIN plugins p ON p.id = i.plugin_id
         JOIN plugin_tools t ON t.plugin_id = p.id
         JOIN users u ON u.id = i.user_id
WHERE i.user_id = $1
  AND p.enabled
  AND (p.audience = 'all' OR u.role = 'admin')
  AND coalesce(p.mcp_url, '') <> ''
  AND (p.auth_type <> 'oauth' OR (i.access_token IS NOT NULL AND NOT i.needs_reconnect))
  AND t.permission <> 'blocked'
ORDER BY p.slug, t.name;

-- name: GetUsablePluginTool :one
-- One tool as ListUsablePluginTools has it, read again at each call so a
-- change made meanwhile counts; no row when it may not be called now.
SELECT p.id          AS plugin_id,
       p.slug,
       p.name        AS plugin_name,
       p.description AS plugin_description,
       p.direct_tools,
       p.icon_text,
       p.icon_color,
       p.icon_path,
       t.name,
       t.title,
       t.description,
       t.input_schema,
       t.read_only,
       t.permission
FROM plugin_installs i
         JOIN plugins p ON p.id = i.plugin_id
         JOIN plugin_tools t ON t.plugin_id = p.id
         JOIN users u ON u.id = i.user_id
WHERE i.user_id = sqlc.arg('user_id')
  AND p.id = sqlc.arg('plugin_id')
  AND t.name = sqlc.arg('tool')
  AND p.enabled
  AND (p.audience = 'all' OR u.role = 'admin')
  AND coalesce(p.mcp_url, '') <> ''
  AND (p.auth_type <> 'oauth' OR (i.access_token IS NOT NULL AND NOT i.needs_reconnect))
  AND t.permission <> 'blocked';

-- name: GetConversationPluginCatalog :one
SELECT plugin_catalog
FROM conversations
WHERE id = $1;

-- name: SetConversationPluginCatalog :one
-- The catalogue a conversation lists from now on, unless another request took
-- it first: then no row, and that one stands.
UPDATE conversations
SET plugin_catalog = $2
WHERE id = $1
  AND plugin_catalog IS NULL
RETURNING plugin_catalog;

-- name: CreatePluginConfirmation :one
INSERT INTO plugin_confirmations (run_id, conversation_id, user_id, call_id, seq, plugin_id, tool, arguments)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: AnswerPluginConfirmation :one
-- The person's answer to a call still waiting in their conversation.
UPDATE plugin_confirmations
SET status      = sqlc.arg('status'),
    answered_at = now()
WHERE id = sqlc.arg('id')
  AND conversation_id = sqlc.arg('conversation_id')
  AND user_id = sqlc.arg('user_id')
  AND status = 'pending'
RETURNING *;

-- name: ExpirePluginConfirmation :exec
UPDATE plugin_confirmations
SET status      = 'expired',
    answered_at = now()
WHERE id = $1
  AND status = 'pending';

-- name: ExpireStalePluginConfirmations :execrows
-- Calls left waiting by a run that is over: a process that died with them.
UPDATE plugin_confirmations c
SET status      = 'expired',
    answered_at = now()
FROM runs r
WHERE r.id = c.run_id
  AND c.status = 'pending'
  AND r.status <> 'running';

-- name: GrantPluginTool :exec
INSERT INTO plugin_tool_grants (conversation_id, plugin_id, tool)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: PluginToolGranted :one
SELECT EXISTS (SELECT 1
               FROM plugin_tool_grants
               WHERE conversation_id = $1
                 AND plugin_id = $2
                 AND tool = $3)::boolean;

-- name: RecordPluginCall :exec
INSERT INTO plugin_calls (user_id, conversation_id, plugin_id, plugin_slug, tool, ok, outcome, confirmed, duration_ms)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);
