-- +goose Up
-- +goose StatementBegin
-- The browser tool (docs/design/browser-plan.md).

-- Whether the person opened the browser for this conversation, from the
-- composer's "+" menu: its runs are offered the browser tool while it is on.
ALTER TABLE conversations ADD COLUMN browser boolean NOT NULL DEFAULT false;

-- A consequential browser action waits for the person as a plugin call does,
-- in the same table: no plugin, the site it is on instead.
ALTER TABLE plugin_confirmations ALTER COLUMN plugin_id DROP NOT NULL;
ALTER TABLE plugin_confirmations ADD COLUMN site text;
ALTER TABLE plugin_confirmations ADD CONSTRAINT plugin_confirmations_subject
    CHECK ((plugin_id IS NULL) <> (site IS NULL));

-- "Allow on this site for the rest of this conversation".
CREATE TABLE browser_site_grants (
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    site            text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, site)
);

-- What the browser did that needed asking, for the administrators, as
-- plugin_calls is: never what was typed.
CREATE TABLE browser_actions (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id         uuid REFERENCES users (id) ON DELETE SET NULL,
    conversation_id uuid REFERENCES conversations (id) ON DELETE SET NULL,
    site            text        NOT NULL,
    action          text        NOT NULL,
    target          text        NOT NULL DEFAULT '',
    outcome         text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX browser_actions_created ON browser_actions (created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE browser_actions;
DROP TABLE browser_site_grants;
DELETE FROM plugin_confirmations WHERE plugin_id IS NULL;
ALTER TABLE plugin_confirmations DROP CONSTRAINT plugin_confirmations_subject;
ALTER TABLE plugin_confirmations DROP COLUMN site;
ALTER TABLE plugin_confirmations ALTER COLUMN plugin_id SET NOT NULL;
ALTER TABLE conversations DROP COLUMN browser;
-- +goose StatementEnd
