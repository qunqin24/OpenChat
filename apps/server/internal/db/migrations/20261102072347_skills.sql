-- +goose Up
-- +goose StatementBegin
-- Skills (docs/design/skills-plan.md): instructions a model loads when a task
-- calls for them, in the Agent Skills format - a SKILL.md with a name and a
-- description in its frontmatter, and the files it refers to. A conversation
-- lists the skills it may use in its snapshot (memory.Snapshot), by version,
-- and the `skill` tool reads them from there.
--
-- A skill is the site's (owner_id NULL, written by an administrator) or one
-- person's own. Every save is a new version; the skill is its current one,
-- and the versions a conversation started with stay readable to it.
CREATE TABLE skills (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id           uuid        REFERENCES users (id) ON DELETE CASCADE,
    -- The frontmatter's name, kept here to be unique: lowercase letters,
    -- digits and single hyphens, at most 64 characters (the spec's rule).
    name               text        NOT NULL,
    enabled            boolean     NOT NULL DEFAULT true,
    current_version_id uuid,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT skills_name CHECK (name ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(name) <= 64)
);
CREATE UNIQUE INDEX skills_site_name_idx ON skills (name) WHERE owner_id IS NULL;
CREATE UNIQUE INDEX skills_owner_name_idx ON skills (owner_id, name) WHERE owner_id IS NOT NULL;

CREATE TABLE skill_versions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id     uuid        NOT NULL REFERENCES skills (id) ON DELETE CASCADE,
    description  text        NOT NULL,
    -- The hash (memory.Hash) of the version's files as content moderation
    -- reads them (skills.Text), and the hash that passed the check - or that
    -- an administrator wrote, which is not checked. A personal version saved
    -- while moderation was off has none, and is checked when a conversation
    -- would take it into its snapshot; blocked is the hash that check refused.
    content_hash text        NOT NULL,
    checked      text,
    blocked      text,
    created_by   uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX skill_versions_skill_idx ON skill_versions (skill_id, created_at);

ALTER TABLE skills
    ADD CONSTRAINT skills_current_version_fk FOREIGN KEY (current_version_id)
        REFERENCES skill_versions (id) DEFERRABLE INITIALLY DEFERRED;

-- A version's files, text only: SKILL.md at the root and what it refers to.
CREATE TABLE skill_files (
    version_id uuid NOT NULL REFERENCES skill_versions (id) ON DELETE CASCADE,
    path       text NOT NULL,
    content    text NOT NULL,
    PRIMARY KEY (version_id, path)
);

-- A person's skill, checked as it is saved: something they wrote for the
-- model to read, so a strike like their custom instructions.
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_source;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_source CHECK (source IN ('send', 'edit', 'answer', 'instructions', 'memory', 'snapshot', 'task', 'task_run', 'document', 'skill'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM moderation_events WHERE source = 'skill';
ALTER TABLE moderation_events DROP CONSTRAINT moderation_events_source;
ALTER TABLE moderation_events
    ADD CONSTRAINT moderation_events_source CHECK (source IN ('send', 'edit', 'answer', 'instructions', 'memory', 'snapshot', 'task', 'task_run', 'document'));
ALTER TABLE skills DROP CONSTRAINT skills_current_version_fk;
DROP TABLE skill_files;
DROP TABLE skill_versions;
DROP TABLE skills;
-- +goose StatementEnd
