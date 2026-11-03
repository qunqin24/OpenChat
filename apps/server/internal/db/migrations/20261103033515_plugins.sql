-- +goose Up
-- +goose StatementBegin
-- Plugins (插件, docs/design/plugins-plan.md): skills plus a remote MCP
-- service, added by administrators and installed by each person from the
-- plugins page. This first step holds the catalogue and who installed what;
-- the MCP connection, OAuth and tokens come next.
CREATE TABLE plugins (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The prefix of the plugin's tool names (`github__create_issue`).
    slug        text        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(slug) <= 32),
    name        text        NOT NULL,
    -- One line, on its tile; `about` is the longer word shown before installing.
    description text        NOT NULL DEFAULT '',
    about       text        NOT NULL DEFAULT '',
    category    text        NOT NULL DEFAULT '',
    -- Its mark until an uploaded logo: a letter or two on a colour.
    icon_text   text        NOT NULL DEFAULT '',
    icon_color  text        NOT NULL DEFAULT '',
    featured    boolean     NOT NULL DEFAULT false,
    enabled     boolean     NOT NULL DEFAULT true,
    audience    text        NOT NULL DEFAULT 'all' CHECK (audience IN ('all', 'admins')),
    mcp_url     text,
    auth_type   text        NOT NULL DEFAULT 'none' CHECK (auth_type IN ('none', 'header', 'oauth')),
    sort        integer     NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- The tools the MCP service lists, as last fetched, each with the
-- administrator's permission: allow, confirm (asked before each call) or blocked.
CREATE TABLE plugin_tools (
    plugin_id   uuid    NOT NULL REFERENCES plugins (id) ON DELETE CASCADE,
    name        text    NOT NULL,
    description text    NOT NULL DEFAULT '',
    read_only   boolean NOT NULL DEFAULT false,
    permission  text    NOT NULL DEFAULT 'confirm' CHECK (permission IN ('allow', 'confirm', 'blocked')),
    PRIMARY KEY (plugin_id, name)
);

CREATE TABLE plugin_installs (
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    plugin_id  uuid        NOT NULL REFERENCES plugins (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, plugin_id)
);
CREATE INDEX plugin_installs_plugin ON plugin_installs (plugin_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE plugin_installs;
DROP TABLE plugin_tools;
DROP TABLE plugins;
-- +goose StatementEnd
