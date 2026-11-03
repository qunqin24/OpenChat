-- +goose Up
-- +goose StatementBegin
-- Plugins beyond a person's own turns (docs/design/plugins-plan.md §9):
-- scheduled runs, sub-agents and researchers, and the skills a plugin brings.

-- A run nobody is there to watch: a scheduled task's. A plugin call that
-- needs confirmation is refused at once instead of waiting for an answer.
ALTER TABLE runs ADD COLUMN unattended boolean NOT NULL DEFAULT false;

-- The plugins a sub-agent may use, named by its launch (short names), as
-- `skills` names its skills. NULL: none.
ALTER TABLE conversations ADD COLUMN plugins text[];

-- A plugin's own skills: written by administrators on the plugin, in a
-- conversation's list only while its owner has the plugin installed. Not a
-- site skill, so the site's names do not collide with them.
ALTER TABLE skills ADD COLUMN plugin_id uuid REFERENCES plugins (id) ON DELETE CASCADE;
ALTER TABLE skills ADD CONSTRAINT skills_plugin_not_personal CHECK (plugin_id IS NULL OR owner_id IS NULL);
DROP INDEX skills_site_name_idx;
CREATE UNIQUE INDEX skills_site_name_idx ON skills (name) WHERE owner_id IS NULL AND plugin_id IS NULL;
CREATE UNIQUE INDEX skills_plugin_name_idx ON skills (plugin_id, name) WHERE plugin_id IS NOT NULL;

-- Pictures a tool answered with (a plugin's screenshot, chart, page): shown
-- under the call and given to the model after the call's result. Small and
-- few (at most a handful of a few MB per call), so kept in the database, and
-- gone with the run whose call fetched them.
CREATE TABLE tool_images (
    id              uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    run_id          uuid        NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    conversation_id uuid        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    media_type      text        NOT NULL CHECK (media_type IN ('image/png', 'image/jpeg', 'image/gif', 'image/webp')),
    data            bytea       NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tool_images_run ON tool_images (run_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE tool_images;
DROP INDEX skills_plugin_name_idx;
DROP INDEX skills_site_name_idx;
DELETE FROM skills WHERE plugin_id IS NOT NULL;
CREATE UNIQUE INDEX skills_site_name_idx ON skills (name) WHERE owner_id IS NULL;
ALTER TABLE skills DROP CONSTRAINT skills_plugin_not_personal;
ALTER TABLE skills DROP COLUMN plugin_id;
ALTER TABLE conversations DROP COLUMN plugins;
ALTER TABLE runs DROP COLUMN unattended;
-- +goose StatementEnd
